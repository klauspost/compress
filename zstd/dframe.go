package zstd

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"runtime"

	"github.com/cespare/xxhash"
)

type dFrame struct {
	br  *bufio.Reader
	crc hash.Hash64

	WindowSize       uint64
	DictionaryID     uint32
	FrameContentSize uint64
	HasCheckSum      bool
	SingleSegment    bool

	// maxWindowSize is the maximum windows size to support.
	// should never be bigger than max-int.
	maxWindowSize uint64
	w             dWindow

	// In order queue of blocks being decoded.
	decoding chan *dBlock

	// Use as little memory as possible.
	lowMem bool
	// Number of in-flight decoders
	concurrent int
}

type dWindow struct {
	w       []byte
	doffset int
}

const (
	// The minimum Window_Size is 1 KB.
	minWindowSize = 1 << 10
	debug         = true
)

var (
	// ErrMagicMismatch is returned when a "magic" number isn't what is expected.
	// Typically this indicates wrong or corrupted input.
	ErrMagicMismatch = errors.New("invalid input: magic number mismatch")

	// ErrWindowSizeExceeded is returned when a "magic" number isn't what is expected.
	// Typically this indicates wrong or corrupted input.
	ErrWindowSizeExceeded = errors.New("window size exceeded")

	// ErrWindowSizeTooSmall is returned when no window size is specified.
	// Typically this indicates wrong or corrupted input.
	ErrWindowSizeTooSmall = errors.New("invalid input: window size was too small")

	// ErrCRCMismatch is returned if CRC mismatches.
	ErrCRCMismatch = errors.New("CRC check failed")

	errNotimplemented = errors.New("not implemented")

	frameMagic          = []byte{0x28, 0xb5, 0x2f, 0xfd}
	skippableFrameMagic = []byte{0x18, 0x4d, 0x2a}
)

func newDFrame() *dFrame {
	d := dFrame{
		maxWindowSize: 1 << 30,
		concurrent:    runtime.GOMAXPROCS(0),
	}
	return &d
}

func (d *dFrame) reset(r io.Reader) error {
	if d.br == nil {
		d.br = bufio.NewReaderSize(r, maxCompressedBlockSize)
	} else {
		d.br.Reset(r)
	}
	if cap(d.decoding) < d.concurrent {
		d.decoding = make(chan *dBlock, d.concurrent)
	}
	var tmp [8]byte
	for {
		_, err := io.ReadFull(d.br, tmp[:4])
		if err != nil {
			return err
		}
		if !bytes.Equal(tmp[:3], skippableFrameMagic) || tmp[3]&0xf0 != 0x50 {
			// Break if not skippable frame.
			break
		}
		// Read size to skip
		_, err = io.ReadFull(d.br, tmp[:4])
		if err != nil {
			return err
		}
		n := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16) | (uint32(tmp[3]) << 24)
		if debug {
			fmt.Println("Skipping frame with", n, "bytes.")
		}
		_, err = io.CopyN(ioutil.Discard, d.br, int64(n))
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(tmp[:4], frameMagic) {
		fmt.Println("Got magic numbers: ", tmp[:4], "want:", frameMagic)
		return ErrMagicMismatch
	}

	// Read Frame_Header_Descriptor
	fhd, err := d.br.ReadByte()
	if err != nil {
		return err
	}
	d.SingleSegment = fhd&(1<<5) != 0

	// Read Window_Descriptor
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#window_descriptor
	d.WindowSize = 0
	if !d.SingleSegment {
		wd, err := d.br.ReadByte()
		if err != nil {
			return err
		}
		if debug {
			fmt.Printf("raw: %x, mantissa: %d, exponent: %d\n", wd, wd&7, wd>>3)
		}
		windowLog := 10 + (wd >> 3)
		windowBase := uint64(1) << windowLog
		windowAdd := (windowBase / 8) * uint64(wd&0x7)
		d.WindowSize = windowBase + windowAdd
	}

	// Read Dictionary_ID
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#dictionary_id
	d.DictionaryID = 0
	if size := fhd & 3; size != 0 {
		if size == 3 {
			size = 4
		}
		_, err := io.ReadFull(d.br, tmp[:size])
		if err != nil {
			return err
		}
		switch size {
		case 1:
			d.DictionaryID = uint32(tmp[0])
		case 2:
			d.DictionaryID = uint32(tmp[0]) | (uint32(tmp[1]) << 8)
		case 4:
			d.DictionaryID = uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16) | (uint32(tmp[3]) << 24)
		}
		if debug {
			fmt.Println("Dict size", size, "ID:", d.DictionaryID)
		}
	}

	// Read Frame_Content_Size
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#frame_content_size
	var fcsSize int
	v := fhd >> 6
	switch v {
	case 0:
		if d.SingleSegment {
			fcsSize = 1
		}
	default:
		fcsSize = 1 << v
	}
	d.FrameContentSize = 0
	if fcsSize > 0 {
		_, err := io.ReadFull(d.br, tmp[:fcsSize])
		if err != nil {
			return err
		}
		switch fcsSize {
		case 1:
			d.FrameContentSize = uint64(tmp[0])
		case 2:
			// When FCS_Field_Size is 2, the offset of 256 is added.
			d.FrameContentSize = uint64(tmp[0]) | (uint64(tmp[1]) << 8) + 256
		case 4:
			d.FrameContentSize = uint64(tmp[0]) | (uint64(tmp[1]) << 8) | (uint64(tmp[2]) << 16) | (uint64(tmp[3] << 24))
		case 8:
			d1 := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16) | (uint32(tmp[3]) << 24)
			d2 := uint32(tmp[4]) | (uint32(tmp[5]) << 8) | (uint32(tmp[6]) << 16) | (uint32(tmp[7]) << 24)
			d.FrameContentSize = uint64(d1) | (uint64(d2) << 32)
		}
		if debug {
			fmt.Println("field size bits:", v, "fcsSize:", fcsSize, "FrameContentSize:", d.FrameContentSize, hex.EncodeToString(tmp[:fcsSize]))
		}
	}
	d.HasCheckSum = fhd&(1<<2) != 0
	if d.HasCheckSum {
		if d.crc == nil {
			d.crc = xxhash.New()
		}
		d.crc.Reset()
	}

	if d.WindowSize == 0 && d.SingleSegment {
		// We may not need window in this case.
		d.WindowSize = d.FrameContentSize
		if d.WindowSize < minWindowSize {
			d.WindowSize = minWindowSize
		}
	}

	if d.WindowSize > d.maxWindowSize {
		if debug {
			fmt.Printf("window size %d > max %d\n", d.WindowSize, d.maxWindowSize)
		}
		return ErrWindowSizeExceeded
	}
	// The minimum Window_Size is 1 KB.
	if d.WindowSize < minWindowSize {
		if debug {
			fmt.Println("got window size: ", d.WindowSize)
		}
		return ErrWindowSizeTooSmall
	}
	if cap(d.w.w) > 0 {
		d.w.w = d.w.w[:0]
	}
	if cap(d.w.w) < int(d.WindowSize) {
		d.w.w = make([]byte, 0, d.WindowSize)
	}
	d.w.w = d.w.w[:d.WindowSize]
	return nil
}

// next will start decoding the next block from stream.
func (d *dFrame) next(block *dBlock) error {
	fmt.Println("decoding new block")
	err := block.reset(d.br, d.WindowSize)
	if err != nil {
		fmt.Println("block error:", err)
		return err
	}
	fmt.Println("next block:", block)
	d.decoding <- block
	if block.Last {
		// FIXME: While this is true for this frame, we cannot rely on this for the stream.
		return io.EOF
	}
	return nil
}

// addDecoder will add another decoder.
// If io.EOF is returned, the added decoder is the last of the frame.
func (d *dFrame) addDecoder(dec *dBlock) error {
	// TODO: We should probably not accept a decoder if we are at end of stream.
	err := dec.reset(d.br, d.WindowSize)
	if err != nil {
		return err
	}
	fmt.Println("added decoder to frame")
	d.decoding <- dec
	if dec.Last {
		return io.EOF
	}
	return nil
}

// checkCRC will check the checksum if the frame has one.
// Will return ErrCRCMismatch if crc check failed, otherwise io.EOF
func (d *dFrame) checkCRC() error {
	if !d.HasCheckSum {
		return io.EOF
	}

	var want [4]byte
	_, err := io.ReadFull(d.br, want[:])
	if err != nil && err != io.EOF {
		return err
	}
	var got [8]byte
	gotB := d.crc.Sum(got[:0])
	// Flip to match file order.
	gotB[0] = gotB[7]
	gotB[1] = gotB[6]
	gotB[2] = gotB[5]
	gotB[3] = gotB[4]
	if !bytes.Equal(gotB[:4], want[:]) {
		fmt.Println(gotB[:4], "!=", want)
		return ErrCRCMismatch
	}
	fmt.Println("CRC ok")
	return io.EOF
}

func (d *dFrame) Close() {
	// TODO: Find some way to signal we are done to startDecoder
}

func (d *dFrame) startDecoder(writer chan decodeOutput) {
	// TODO: Init to dictionary
	history := &buffer{}
	// Get first block
	block := <-d.decoding
	block.history <- history
	for {
		var next *dBlock
		// Get result
		r := <-block.result
		if r.err != nil {
			writer <- r
		}
		if !block.Last {
			// Send history to next block
			next = <-d.decoding
			next.history <- history
		}

		// Add checksum
		if d.HasCheckSum {
			n, err := d.crc.Write(r.b)
			if err != nil {
				r.err = err
				if n != len(r.b) {
					r.err = io.ErrShortWrite
				}
				writer <- r
			}
		}
		if block.Last {
			r.err = d.checkCRC()
			writer <- r
			return
		}
		writer <- r
		block = next
	}
}
