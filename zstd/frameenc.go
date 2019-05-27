// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import "math/bits"

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
	if f.Checksum {
		fhd |= 1 << 2
	}
	if f.SingleSegment {
		fhd |= 1 << 5
	}
	// TODO: add the rest
	const winLogMin = 10
	windowLog := (bits.Len32(f.WindowSize-1) - winLogMin) << 3
	dst = append(dst, fhd, uint8(windowLog))
	return dst, nil
}
