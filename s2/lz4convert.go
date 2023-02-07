// Copyright (c) 2022 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// LZ4Converter provides conversion from LZ4 blocks as defined here:
// https://github.com/lz4/lz4/blob/dev/doc/lz4_Block_format.md
type LZ4Converter struct {
}

// ErrDstTooSmall is returned when provided destination is too small.
var ErrDstTooSmall = errors.New("s2: destination too small")

// ConvertBlock will convert an LZ4 block and append it as an S2
// block without block length to dst.
// The uncompressed size is returned as well.
// dst must have capacity to contain the entire compressed block.
// Use MaxEncodedLen(uncompressed_size) if in doubt.
func (l *LZ4Converter) ConvertBlock(dst, src []byte) ([]byte, int, error) {
	if len(src) == 0 {
		return dst, 0, nil
	}
	const debug = false
	const lz4MinMatch = 4

	s, d := 0, len(dst)
	dst = dst[:cap(dst)]
	dLimit := len(dst) - 8
	var lastOffset, uncompressed int
	if debug {
		fmt.Printf("convert block start: len(src): %d, len(dst):%d \n", len(src), len(dst))
	}

	for s < len(src) {
		// Read literal info
		token := src[s]
		ll := int(token >> 4)
		ml := int(lz4MinMatch + (token & 0xf))

		// If upper nibble is 15, literal length is extended
		if token >= 0xf0 {
			for {
				s++
				if s >= len(src) {
					if debug {
						fmt.Printf("error reading ll: s (%d) >= len(src) (%d)\n", s, len(src))
					}
					return dst[:d], 0, ErrCorrupt
				}
				val := src[s]
				ll += int(val)
				if val != 255 {
					break
				}
			}
		}
		// Skip past token
		if s+ll >= len(src) {
			if debug {
				fmt.Printf("error literals: s+ll (%d+%d) >= len(src) (%d)\n", s, ll, len(src))
			}
			return dst[:d], 0, ErrCorrupt
		}
		s++
		if ll > 0 {
			if d+ll > dLimit {
				return dst[:d], 0, ErrDstTooSmall
			}
			if debug {
				fmt.Printf("emit %d literals\n", ll)
			}
			d += emitLiteral(dst[d:], src[s:s+ll])
			s += ll
			uncompressed += ll

			if d > dLimit {
				return dst[:d], 0, ErrDstTooSmall
			}
		}

		// Check if we are done...
		if s == len(src) && ml == lz4MinMatch {
			break
		}
		// 2 byte offset
		if s >= len(src)-2 {
			return dst[:d], 0, ErrCorrupt
		}
		offset := int(binary.LittleEndian.Uint16(src[s:]))
		s += 2
		if offset == 0 {
			if debug {
				fmt.Printf("error: offset 0, ml: %d, len(src)-s: %d\n", ml, len(src)-s)
			}
			return dst[:d], 0, ErrCorrupt
		}
		if offset > uncompressed {
			if debug {
				fmt.Printf("error: offset (%d)> uncompressed (%d)\n", offset, uncompressed)
			}
			return dst[:d], 0, ErrCorrupt
		}

		if ml == lz4MinMatch+15 {
			for {
				if s >= len(src) {
					if debug {
						fmt.Printf("error reading ml: s (%d) >= len(src) (%d)\n", s, len(src))
					}
					return dst[:d], 0, ErrCorrupt
				}
				val := src[s]
				s++
				ml += int(val)
				if val != 255 {
					if s >= len(src) {
						if debug {
							fmt.Printf("error reading ml: s (%d) >= len(src) (%d)\n", s, len(src))
						}
						return dst[:d], 0, ErrCorrupt
					}
					break
				}
			}
		}
		if offset == lastOffset {
			if debug {
				fmt.Printf("emit repeat, length: %d, offset: %d\n", ml, offset)
			}
			d += emitRepeat(dst[d:], offset, ml)
		} else {
			if debug {
				fmt.Printf("emit copy, length: %d, offset: %d\n", ml, offset)
			}
			d += emitCopy(dst[d:], offset, ml)
			lastOffset = offset
		}
		uncompressed += ml
		if d > dLimit {
			return dst[:d], 0, ErrDstTooSmall
		}
	}

	return dst[:d], uncompressed, nil
}
