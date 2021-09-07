// Copyright 2016 The Snappy-Go Authors. All rights reserved.
// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || appengine || !gc || noasm
// +build !amd64,!arm64 appengine !gc noasm

package s2

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

// decode writes the decoding of src to dst. It assumes that the varint-encoded
// length of the decompressed bytes has already been read, and that len(dst)
// equals that length.
//
// It returns 0 on success or a decodeErrCodeXxx error code on failure.
func s2Decode(dst, src []byte) int {
	const debug = false
	if debug {
		fmt.Println("Starting decode, dst len:", len(dst))
	}
	var d, s, length int
	offset := 0

	// As long as we can read at least 5 bytes...
	for s < len(src)-5 {
		// None of these 2 should have bounds checks, but both have with Go 1.17
		tmp := src[s : s+5]
		tmp = tmp[:5]
		switch tmp[0] & 0x03 {
		case tagLiteral:
			x := uint32(tmp[0] >> 2)
			switch {
			case x < 60:
				s++
			case x == 60:
				x = uint32(tmp[1])
				s += 2
			case x == 61:
				x = uint32(binary.LittleEndian.Uint16(tmp[1:]))
				s += 3
			case x == 62:
				x = binary.LittleEndian.Uint32(tmp) >> 8
				s += 4
			case x == 63:
				x = binary.LittleEndian.Uint32(tmp[1:])
				s += 5
			}
			length = int(x) + 1
			if length > len(dst)-d || length > len(src)-s || (strconv.IntSize == 32 && length <= 0) {
				return decodeErrCodeCorrupt
			}
			if debug {
				fmt.Println("literals, length:", length, "d-after:", d+length)
			}

			copy(dst[d:], src[s:s+length])
			d += length
			s += length
			continue

		case tagCopy1:
			length = int(tmp[0]) >> 2 & 0x7
			toffset := int(uint32(tmp[0])&0xe0<<3 | uint32(tmp[0+1]))
			s += 2
			if toffset == 0 {
				if debug {
					fmt.Print("(repeat) ")
				}
				// keep last offset
				switch length {
				case 5:
					length = int(tmp[2]) + 4
					s += 1
				case 6:
					length = int(binary.LittleEndian.Uint16(tmp[2:])) + (1 << 8)
					s += 2
				case 7:
					length = int(binary.LittleEndian.Uint32(tmp[1:])>>8) + (1 << 16)
					s += 3
				default: // 0-> 4
				}
			} else {
				offset = toffset
			}
			length += 4
		case tagCopy2:
			length = 1 + int(tmp[0])>>2
			offset = int(binary.LittleEndian.Uint16(tmp[1:]))
			s += 3

		case tagCopy4:
			length = 1 + int(tmp[0])>>2
			offset = int(binary.LittleEndian.Uint32(tmp[1:]))
			s += 5
		}

		if offset <= 0 || d < offset || length > len(dst)-d {
			return decodeErrCodeCorrupt
		}

		if debug {
			fmt.Println("copy, length:", length, "offset:", offset, "d-after:", d+length)
		}

		// Copy from an earlier sub-slice of dst to a later sub-slice.
		// If no overlap, use the built-in copy:
		if offset > length {
			copy(dst[d:d+length], dst[d-offset:])
			d += length
			continue
		}

		// Unlike the built-in copy function, this byte-by-byte copy always runs
		// forwards, even if the slices overlap. Conceptually, this is:
		//
		// d += forwardCopy(dst[d:d+length], dst[d-offset:])
		//
		// We align the slices into a and b and show the compiler they are the same size.
		// This allows the loop to run without bounds checks.
		a := dst[d : d+length]
		b := dst[d-offset:]
		b = b[:len(a)]
		for i := range a {
			a[i] = b[i]
		}
		d += length
	}

	// Remaining with extra checks...
	for s < len(src) {
		switch src[s] & 0x03 {
		case tagLiteral:
			x := uint32(src[s] >> 2)
			switch {
			case x < 60:
				s++
			case x == 60:
				s += 2
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-1])
			case x == 61:
				s += 3
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-2]) | uint32(src[s-1])<<8
			case x == 62:
				s += 4
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-3]) | uint32(src[s-2])<<8 | uint32(src[s-1])<<16
			case x == 63:
				s += 5
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return decodeErrCodeCorrupt
				}
				x = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
			}
			length = int(x) + 1
			if length > len(dst)-d || length > len(src)-s || (strconv.IntSize == 32 && length <= 0) {
				return decodeErrCodeCorrupt
			}
			if debug {
				fmt.Println("literals, length:", length, "d-after:", d+length)
			}

			copy(dst[d:], src[s:s+length])
			d += length
			s += length
			continue

		case tagCopy1:
			s += 2
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = int(src[s-2]) >> 2 & 0x7
			toffset := int(uint32(src[s-2])&0xe0<<3 | uint32(src[s-1]))
			if toffset == 0 {
				if debug {
					fmt.Print("(repeat) ")
				}
				// keep last offset
				switch length {
				case 5:
					s += 1
					if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
						return decodeErrCodeCorrupt
					}
					length = int(uint32(src[s-1])) + 4
				case 6:
					s += 2
					if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
						return decodeErrCodeCorrupt
					}
					length = int(uint32(src[s-2])|(uint32(src[s-1])<<8)) + (1 << 8)
				case 7:
					s += 3
					if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
						return decodeErrCodeCorrupt
					}
					length = int(uint32(src[s-3])|(uint32(src[s-2])<<8)|(uint32(src[s-1])<<16)) + (1 << 16)
				default: // 0-> 4
				}
			} else {
				offset = toffset
			}
			length += 4
		case tagCopy2:
			s += 3
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = 1 + int(src[s-3])>>2
			offset = int(uint32(src[s-2]) | uint32(src[s-1])<<8)

		case tagCopy4:
			s += 5
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return decodeErrCodeCorrupt
			}
			length = 1 + int(src[s-5])>>2
			offset = int(uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24)
		}

		if offset <= 0 || d < offset || length > len(dst)-d {
			return decodeErrCodeCorrupt
		}

		if debug {
			fmt.Println("copy, length:", length, "offset:", offset, "d-after:", d+length)
		}

		// Copy from an earlier sub-slice of dst to a later sub-slice.
		// If no overlap, use the built-in copy:
		if offset > length {
			copy(dst[d:d+length], dst[d-offset:])
			d += length
			continue
		}

		// Unlike the built-in copy function, this byte-by-byte copy always runs
		// forwards, even if the slices overlap. Conceptually, this is:
		//
		// d += forwardCopy(dst[d:d+length], dst[d-offset:])
		//
		// We align the slices into a and b and show the compiler they are the same size.
		// This allows the loop to run without bounds checks.
		a := dst[d : d+length]
		b := dst[d-offset:]
		b = b[:len(a)]
		for i := range a {
			a[i] = b[i]
		}
		d += length
	}

	if d != len(dst) {
		return decodeErrCodeCorrupt
	}
	return 0
}
