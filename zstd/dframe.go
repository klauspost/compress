package zstd

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/ioutil"

	"github.com/cespare/xxhash"
)

type dFrame struct {
	o   decoderOptions
	br  *bufio.Reader
	crc hash.Hash64
	//stopDecoding chan struct{}
	frameDone chan *bufio.Reader
	tmp       [8]byte

	WindowSize       uint64
	DictionaryID     uint32
	FrameContentSize uint64
	HasCheckSum      bool
	SingleSegment    bool

	// maxWindowSize is the maximum windows size to support.
	// should never be bigger than max-int.
	maxWindowSize uint64

	// In order queue of blocks being decoded.
	decoding chan *dBlock

	// Frame history passed between blocks
	history history
}

const (
	// The minimum Window_Size is 1 KB.
	minWindowSize = 1 << 10
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

func newDFrame(o decoderOptions) *dFrame {
	d := dFrame{
		o:             o,
		maxWindowSize: 1 << 30,
		//	stopDecoding:  make(chan struct{}, 0),
		frameDone: make(chan *bufio.Reader, 0),
	}
	return &d
}

// reset will read the frame header and prepare for block decoding.
// If nothing can be read from the input, io.EOF will be returned.
// Any other error indicated that the stream contained data, but
// there was a problem.
func (d *dFrame) reset(r io.Reader) error {
	if br, ok := r.(*bufio.Reader); ok {
		d.br = br
	} else {
		if d.br == nil {
			if d.o.lowMem {
				// Use 4K default.
				d.br = bufio.NewReader(r)
			} else {
				d.br = bufio.NewReaderSize(r, maxCompressedBlockSize)
			}
		} else {
			d.br.Reset(r)
		}
	}
	if cap(d.decoding) < d.o.concurrent {
		d.decoding = make(chan *dBlock, d.o.concurrent)
	}
	d.HasCheckSum = false
	d.WindowSize = 0
	for {
		_, err := io.ReadFull(d.br, d.tmp[:4])
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				return io.EOF
			}
			println("Reading Frame Magic", err)
			return err
		}
		if !bytes.Equal(d.tmp[:3], skippableFrameMagic) || d.tmp[3]&0xf0 != 0x50 {
			// Break if not skippable frame.
			break
		}
		// Read size to skip
		_, err = io.ReadFull(d.br, d.tmp[:4])
		if err != nil {
			println("Reading Frame Size", err)
			return err
		}
		n := uint32(d.tmp[0]) | (uint32(d.tmp[1]) << 8) | (uint32(d.tmp[2]) << 16) | (uint32(d.tmp[3]) << 24)
		println("Skipping frame with", n, "bytes.")
		_, err = io.CopyN(ioutil.Discard, d.br, int64(n))
		if err != nil {
			if debug {
				println("Reading discarded frame", err)
			}
			return err
		}
	}
	if !bytes.Equal(d.tmp[:4], frameMagic) {
		println("Got magic numbers: ", d.tmp[:4], "want:", frameMagic)
		return ErrMagicMismatch
	}

	// Read Frame_Header_Descriptor
	fhd, err := d.br.ReadByte()
	if err != nil {
		println("Reading Frame_Header_Descriptor", err)
		return err
	}
	d.SingleSegment = fhd&(1<<5) != 0

	// Read Window_Descriptor
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#window_descriptor
	d.WindowSize = 0
	if !d.SingleSegment {
		wd, err := d.br.ReadByte()
		if err != nil {
			println("Reading Window_Descriptor", err)
			return err
		}
		printf("raw: %x, mantissa: %d, exponent: %d\n", wd, wd&7, wd>>3)
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
		_, err := io.ReadFull(d.br, d.tmp[:size])
		if err != nil {
			if debug {
				println("Reading Dictionary_ID", err)
			}
			return err
		}
		switch size {
		case 1:
			d.DictionaryID = uint32(d.tmp[0])
		case 2:
			d.DictionaryID = uint32(d.tmp[0]) | (uint32(d.tmp[1]) << 8)
		case 4:
			d.DictionaryID = uint32(d.tmp[0]) | (uint32(d.tmp[1]) << 8) | (uint32(d.tmp[2]) << 16) | (uint32(d.tmp[3]) << 24)
		}
		if debug {
			println("Dict size", size, "ID:", d.DictionaryID)
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
		_, err := io.ReadFull(d.br, d.tmp[:fcsSize])
		if err != nil {
			println("Reading Frame content", err)
			return err
		}
		switch fcsSize {
		case 1:
			d.FrameContentSize = uint64(d.tmp[0])
		case 2:
			// When FCS_Field_Size is 2, the offset of 256 is added.
			d.FrameContentSize = uint64(d.tmp[0]) | (uint64(d.tmp[1]) << 8) + 256
		case 4:
			d.FrameContentSize = uint64(d.tmp[0]) | (uint64(d.tmp[1]) << 8) | (uint64(d.tmp[2]) << 16) | (uint64(d.tmp[3] << 24))
		case 8:
			d1 := uint32(d.tmp[0]) | (uint32(d.tmp[1]) << 8) | (uint32(d.tmp[2]) << 16) | (uint32(d.tmp[3]) << 24)
			d2 := uint32(d.tmp[4]) | (uint32(d.tmp[5]) << 8) | (uint32(d.tmp[6]) << 16) | (uint32(d.tmp[7]) << 24)
			d.FrameContentSize = uint64(d1) | (uint64(d2) << 32)
		}
		if debug {
			println("field size bits:", v, "fcsSize:", fcsSize, "FrameContentSize:", d.FrameContentSize, hex.EncodeToString(d.tmp[:fcsSize]))
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
		printf("window size %d > max %d\n", d.WindowSize, d.maxWindowSize)
		return ErrWindowSizeExceeded
	}
	// The minimum Window_Size is 1 KB.
	if d.WindowSize < minWindowSize {
		println("got window size: ", d.WindowSize)
		return ErrWindowSizeTooSmall
	}
	d.history.windowSize = int(d.WindowSize)
	d.history.maxSize = d.history.windowSize + maxBlockSize
	if !d.o.lowMem && !d.SingleSegment {
		// set max extra size history to 20MB.
		d.history.maxSize = d.history.windowSize + maxBlockSize*10
	}
	// re-alloc if more than one extra block size.
	if d.o.lowMem && cap(d.history.b) > d.history.maxSize+maxBlockSize {
		d.history.b = make([]byte, 0, d.history.maxSize)
	}
	if cap(d.history.b) < d.history.maxSize {
		d.history.b = make([]byte, 0, d.history.maxSize)
	}
	return nil
}

// next will start decoding the next block from stream.
func (d *dFrame) next(block *dBlock) error {
	println("decoding new block")
	err := block.reset(d.br, d.WindowSize)
	if err != nil {
		println("block error:", err)
		return err
	}
	println("next block:", block)
	if block.Last {
		// We indicate the frame is done by sending io.EOF
		d.decoding <- block
		return io.EOF
	}
	d.decoding <- block
	return nil
}

func (d *dFrame) sendEOS(block *dBlock) {
	println("sending EOS")
	block.sendEOS()
	d.decoding <- block
}

/*
// addDecoder will add another decoder that is decoding the next block.
// If io.EOF is returned, the added decoder is decoding
// the last block of the frame.
func (d *dFrame) addDecoder(dec *dBlock) error {
	err := dec.reset(d.br, d.WindowSize)
	if err != nil {
		d.stopDecoding <- struct{}{}
		return err
	}
	println("added decoder to frame")
	d.decoding <- dec
	if dec.Last {
		return io.EOF
	}
	return nil
}
*/

// checkCRC will check the checksum if the frame has one.
// Will return ErrCRCMismatch if crc check failed, otherwise nil.
func (d *dFrame) checkCRC() error {
	if !d.HasCheckSum {
		return nil
	}

	gotB := d.crc.Sum(d.tmp[:0])
	// Flip to match file order.
	gotB[0] = gotB[7]
	gotB[1] = gotB[6]
	gotB[2] = gotB[5]
	gotB[3] = gotB[4]

	// We can overwrite upper tmp now
	_, err := io.ReadFull(d.br, d.tmp[4:])
	if err != nil {
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		return err
	}

	if !bytes.Equal(gotB[:4], d.tmp[4:]) {
		println("CRC Check Failed:", gotB[:4], "!=", d.tmp[4:])
		return ErrCRCMismatch
	}
	println("CRC ok")
	return nil
}

func (d *dFrame) Close() {
	// TODO: Find some way to signal we are done to startDecoder
}

// startDecoder will start decoding blocks and write them to the writer.
// The decoder will stop as soon as an error occurs or at end of frame.
// When the frame has finished decoding the *bufio.Reader
// containing the remaining input will be sent on dFrame.frameDone.
func (d *dFrame) startDecoder(stream decodeStream) {
	// TODO: Init to dictionary
	d.history.reset()
	defer func() {
		println("frame decoder done, sending remaining bit stream")
		d.frameDone <- d.br
	}()
	// Get decoder for first block.
	block := <-d.decoding
	block.history <- &d.history
	for {
		var next *dBlock
		// Get result
		r := <-block.result
		if r.err != nil {
			println("Result contained error", r.err)
			stream.output <- r
			return
		}
		if !block.Last {
			// Send history to next block
			select {
			case next = <-d.decoding:
				if debug {
					println("Sending ", len(d.history.b), " bytes as history")
				}
				next.history <- &d.history
			default:
				// Wait until we have sent the block, so
				// other decoders can potentially get the decoder.
				next = nil
			}
		}

		// Add checksum, async to decoding.
		if d.HasCheckSum {
			n, err := d.crc.Write(r.b)
			if err != nil {
				r.err = err
				if n != len(r.b) {
					r.err = io.ErrShortWrite
				}
				stream.output <- r
			}
		}
		if block.Last {
			r.err = d.checkCRC()
			stream.output <- r
			return
		}
		stream.output <- r
		if next == nil {
			// There was no decoder available, we wait for one now that we have sent to the writer.
			if debug {
				println("Sending ", len(d.history.b), " bytes as history")
			}
			next = <-d.decoding
			next.history <- &d.history
		}
		block = next
	}
}
