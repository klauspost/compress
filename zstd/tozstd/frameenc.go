package tozstd

import "math/bits"

var frameMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

type frameHeader struct {
	ContentSize   uint64
	WindowSize    uint32
	SingleSegment bool
	Checksum      bool
	DictID        uint32
}

func (f frameHeader) appendTo(dst []byte) ([]byte, error) {
	dst = append(dst, frameMagic...)
	var fhd uint8
	// TODO: add the rest
	const winLogMin = 10
	windowLog := (bits.Len32(f.WindowSize-1) - winLogMin) << 3
	dst = append(dst, fhd, uint8(windowLog))
	return dst, nil
}
