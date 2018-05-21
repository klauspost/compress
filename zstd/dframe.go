package zstd

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

type dFrame struct {
	br *bufio.Reader

	WindowSize       uint64
	DictionaryID     uint32
	FrameContentSize uint64
	HasCheckSum      bool
	SingleSegment    bool

	// maxWindowSize is the maximum windows size to support.
	// should never be bigger than max-int.
	maxWindowSize uint64
	w             dWindow
}

type dWindow struct {
	w       []byte
	doffset int
}

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

	frameMagic = []byte{0xfd, 0x2f, 0xb5, 0x28}
)

func newDFrame(r io.Reader) (*dFrame, error) {
	d := dFrame{maxWindowSize: 1 << 30}
	d.reset(r)
	return &d, nil
}

func (d *dFrame) reset(r io.Reader) error {
	if d.br == nil {
		d.br = bufio.NewReader(r)
	} else {
		d.br.Reset(r)
	}
	var tmp [8]byte
	_, err := io.ReadFull(d.br, tmp[:4])
	if err != nil {
		return err
	}
	if !bytes.Equal(tmp[:4], frameMagic) {
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
	if fc := fhd & (1 << 5); fc != 0 {
		wd, err := d.br.ReadByte()
		if err != nil {
			return err
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
	}

	// Read Frame_Content_Size
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#frame_content_size
	fcsSize := fhd >> 6
	if fcsSize == 0 {
		if fhd&(1<<5) != 0 {
			fcsSize = 1
		}
	} else {
		fcsSize = 1 << fcsSize
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
			d.FrameContentSize = uint64(tmp[0]) | uint64(tmp[1]<<8) + 256
		case 4:
			d.FrameContentSize = uint64(tmp[0]) | uint64(tmp[1]<<8) | uint64(tmp[2]<<16) | uint64(tmp[3]<<24)
		case 8:
			d1 := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16) | (uint32(tmp[3]) << 24)
			d2 := uint32(tmp[4]) | (uint32(tmp[5]) << 8) | (uint32(tmp[6]) << 16) | (uint32(tmp[7]) << 24)
			d.FrameContentSize = uint64(d1) | (uint64(d2) << 32)
		}
	}
	d.HasCheckSum = fhd&(1<<2) != 0
	if d.WindowSize == 0 && d.SingleSegment {
		// We may not need window in this case.
		d.WindowSize = d.FrameContentSize
	}
	if d.WindowSize > d.maxWindowSize {
		return ErrWindowSizeExceeded
	}
	// The minimum Window_Size is 1 KB.
	if d.WindowSize < (1 << 10) {
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
