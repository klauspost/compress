// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"errors"
	"math/bits"
)

type frameHeader struct {
	ContentSize   uint64
	WindowSize    uint32
	SingleSegment bool
	Checksum      bool
	DictID        uint32 // Not stored.
}

const maxHeaderSize = 14

func (f frameHeader) appendTo(dst []byte) ([]byte, error) {
	dst = append(dst, frameMagic...)
	var fhd uint8
	if f.Checksum {
		fhd |= 1 << 2
	}
	if f.SingleSegment {
		fhd |= 1 << 5
	}
	var fcs uint8
	if f.ContentSize >= 256 {
		fcs++
	}
	if f.ContentSize >= 65536+256 {
		fcs++
	}
	if f.ContentSize >= 0xffffffff {
		fcs++
	}
	fhd |= fcs << 6

	dst = append(dst, fhd)
	if !f.SingleSegment {
		const winLogMin = 10
		windowLog := (bits.Len32(f.WindowSize-1) - winLogMin) << 3
		dst = append(dst, uint8(windowLog))
	}
	if f.SingleSegment && f.ContentSize == 0 {
		return nil, errors.New("single segment, but no size set")
	}
	switch fcs {
	case 0:
		if f.SingleSegment {
			dst = append(dst, uint8(f.ContentSize))
		}
		// Unless SingleSegment is set, framessizes < 256 are nto stored.
	case 1:
		f.ContentSize -= 256
		dst = append(dst, uint8(f.ContentSize), uint8(f.ContentSize>>8))
	case 2:
		dst = append(dst, uint8(f.ContentSize), uint8(f.ContentSize>>8), uint8(f.ContentSize>>16), uint8(f.ContentSize>>24))
	case 3:
		dst = append(dst, uint8(f.ContentSize), uint8(f.ContentSize>>8), uint8(f.ContentSize>>16), uint8(f.ContentSize>>24),
			uint8(f.ContentSize>>32), uint8(f.ContentSize>>40), uint8(f.ContentSize>>48), uint8(f.ContentSize>>56))
	default:
		panic("invalid fcs")
	}
	return dst, nil
}
