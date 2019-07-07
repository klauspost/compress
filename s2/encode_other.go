// Copyright 2016 The Snappy-Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"math/bits"
)

func load32(b []byte, i int) uint32 {
	b = b[i:]
	b = b[:4]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func load64(b []byte, i int) uint64 {
	b = b[i:]
	b = b[:8]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// emitLiteral writes a literal chunk and returns the number of bytes written.
//
// It assumes that:
//	dst is long enough to hold the encoded bytes
//	1 <= len(lit) && len(lit) <= 65536
func emitLiteral(dst, lit []byte) int {
	//fmt.Println("emit lits:", len(lit)-1)
	if len(lit) == 0 {
		return 0
	}
	i, n := 0, uint(len(lit)-1)
	switch {
	case n < 60:
		dst[0] = uint8(n)<<2 | tagLiteral
		i = 1
	case n < 1<<8:
		dst[0] = 60<<2 | tagLiteral
		dst[1] = uint8(n)
		i = 2
	case n < 1<<16:
		dst[0] = 61<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		i = 3
	case n < 1<<24:
		dst[0] = 62<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		dst[3] = uint8(n >> 16)
		i = 4
	default:
		dst[0] = 63<<2 | tagLiteral
		dst[1] = uint8(n)
		dst[2] = uint8(n >> 8)
		dst[3] = uint8(n >> 16)
		dst[4] = uint8(n >> 24)
		i = 5
	}
	return i + copy(dst[i:], lit)
}

// emitCopy writes a copy chunk and returns the number of bytes written.
//
// It assumes that:
//	dst is long enough to hold the encoded bytes
//	1 <= offset && offset <= 65535
//	4 <= length && length <= 65535
func emitCopy(dst []byte, offset, length int) int {
	i := 0
	// The maximum length for a single tagCopy1 or tagCopy2 op is 64 bytes. The
	// threshold for this loop is a little higher (at 68 = 64 + 4), and the
	// length emitted down below is is a little lower (at 60 = 64 - 4), because
	// it's shorter to encode a length 67 copy as a length 60 tagCopy2 followed
	// by a length 7 tagCopy1 (which encodes as 3+2 bytes) than to encode it as
	// a length 64 tagCopy2 followed by a length 3 tagCopy2 (which encodes as
	// 3+3 bytes). The magic 4 in the 64±4 is because the minimum length for a
	// tagCopy1 op is 4 bytes, which is why a length 3 copy has to be an
	// encodes-as-3-bytes tagCopy2 instead of an encodes-as-2-bytes tagCopy1.
	if offset >= 65536 {
		for length >= 64 {
			// Emit a length 64 copy, encoded as 5 bytes.
			dst[i+0] = 63<<2 | tagCopy4
			dst[i+1] = uint8(offset)
			dst[i+2] = uint8(offset >> 8)
			dst[i+3] = uint8(offset >> 16)
			dst[i+4] = uint8(offset >> 24)
			i += 5
			length -= 64
		}
		if length == 0 {
			return i
		}
		// Emit a length 64 copy, encoded as 5 bytes.
		dst[i+0] = uint8(length-1)<<2 | tagCopy4
		dst[i+1] = uint8(offset)
		dst[i+2] = uint8(offset >> 8)
		dst[i+3] = uint8(offset >> 16)
		dst[i+4] = uint8(offset >> 24)
		return i + 5
	}
	for length >= 68 {
		// Emit a length 64 copy, encoded as 3 bytes.
		dst[i+0] = 63<<2 | tagCopy2
		dst[i+1] = uint8(offset)
		dst[i+2] = uint8(offset >> 8)
		i += 3
		length -= 64
	}
	if length > 64 {
		// Emit a length 60 copy, encoded as 3 bytes.
		dst[i+0] = 59<<2 | tagCopy2
		dst[i+1] = uint8(offset)
		dst[i+2] = uint8(offset >> 8)
		i += 3
		length -= 60
	}
	if length >= 12 || offset >= 2048 {
		// Emit the remaining copy, encoded as 3 bytes.
		dst[i+0] = uint8(length-1)<<2 | tagCopy2
		dst[i+1] = uint8(offset)
		dst[i+2] = uint8(offset >> 8)
		return i + 3
	}
	// Emit the remaining copy, encoded as 2 bytes.
	dst[i+0] = uint8(offset>>8)<<5 | uint8(length-4)<<2 | tagCopy1
	dst[i+1] = uint8(offset)
	return i + 2
}

// emitRepeat writes a copy chunk and returns the number of bytes written.
func emitRepeat(dst []byte, offset, length int) int {
	i := 0
	// Repeat offset, make length cheaper
	length -= 4
	if length <= 4 {
		dst[i+0] = uint8(length)<<2 | tagCopy1
		dst[i+1] = 0
		return i + 2
	}
	if length < 8 && offset < 2048 {
		// Encode WITH offset
		dst[i+0] = uint8(offset>>8)<<5 | uint8(length)<<2 | tagCopy1
		dst[i+1] = uint8(offset)
		return i + 2
	}
	if length < 1<<8 {
		length -= 4
		dst[i+0] = 5<<2 | tagCopy1
		dst[i+1] = 0
		dst[i+2] = uint8(length)
		return i + 3
	}
	if length < (1 << 16) {
		length -= 1 << 8
		dst[i+0] = 6<<2 | tagCopy1
		dst[i+1] = 0
		dst[i+2] = uint8(length >> 0)
		dst[i+3] = uint8(length >> 8)
		return i + 4
	}
	length -= 1 << 16
	dst[i+0] = 7<<2 | tagCopy1
	dst[i+1] = 0
	dst[i+2] = uint8(length >> 0)
	dst[i+3] = uint8(length >> 8)
	dst[i+4] = uint8(length >> 16)
	return i + 5
}

// extendMatch returns the largest k such that k <= len(src) and that
// src[i:i+k-j] and src[j:k] have the same contents.
//
// It assumes that:
//	0 <= i && i < j && j <= len(src)
func extendMatch(src []byte, i, j int) int {
	for ; j < len(src) && src[i] == src[j]; i, j = i+1, j+1 {
	}
	return j
}

func hash(u, shift uint32) uint32 {
	return (u * 0x1e35a7bd) >> shift
}

// hash5 returns the hash of the lowest 5 bytes of u to fit in a hash table with h bits.
// Preferably h should be a constant and should always be <64.
func hash5(u uint64, h uint8) uint32 {
	const prime5bytes = 889523592379
	return uint32(((u << (64 - 40)) * prime5bytes) >> ((64 - h) & 63))
}

// hash6 returns the hash of the lowest 6 bytes of u to fit in a hash table with h bits.
// Preferably h should be a constant and should always be <64.
func hash6(u uint64, h uint8) uint32 {
	const prime6bytes = 227718039650203
	return uint32(((u << (64 - 48)) * prime6bytes) >> ((64 - h) & 63))
}

// encodeBlock encodes a non-empty src to a guaranteed-large-enough dst. It
// assumes that the varint-encoded length of the decompressed bytes has already
// been written.
//
// It also assumes that:
//	len(dst) >= MaxEncodedLen(len(src)) &&
// 	minNonLiteralBlockSize <= len(src) && len(src) <= maxBlockSize
func encodeBlock(dst, src []byte) (d int) {
	// Initialize the hash table.
	const (
		tableBits = 14

		maxTableSize = 1 << tableBits
		// tableMask is redundant, but helps the compiler eliminate bounds
		// checks.
		tableMask = maxTableSize - 1
	)
	if len(src) <= 32 {
		return emitLiteral(dst, src)
	}
	// In Go, all array elements are zero-initialized, so there is no advantage
	// to a smaller tableSize per se. However, it matches the C++ algorithm,
	// and in the asm versions of this code, we can get away with zeroing only
	// the first tableSize elements.
	var table [maxTableSize]uint32

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := len(src) - inputMargin

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := 0

	// The encoded form must start with a literal, as there are no previous
	// bytes to copy, so we start looking for hash matches at s == 1.
	s := 0
	for ; s <= 32-inputMargin; s += 3 {
		x := load64(src, s)
		h0 := hash6(x, tableBits)
		h1 := hash6(x>>8, tableBits)
		h2 := hash6(x>>16, tableBits)
		table[h0&tableMask] = uint32(s)
		table[h1&tableMask] = uint32(s + 1)
		table[h2&tableMask] = uint32(s + 2)
	}

	nextHash := hash6(load64(src, s), tableBits)
	repeat := 0

	for {
		candidate := 0
		for {
			// Next src position to check
			nextS := s + (s-nextEmit)>>7 + 1
			if nextS > sLimit {
				goto emitRemainder
			}
			x := load64(src, s)
			candidate = int(table[nextHash&tableMask])
			table[nextHash&tableMask] = uint32(s)
			nextHash = hash6(load64(src, nextS), tableBits)

			// Check repeat.
			if false && uint32(x>>8) == load32(src, s-repeat+1) && repeat > 0 {
				base := s + 1
				// Extend back
				for i := base - repeat; base > nextEmit && src[i-1] == src[base-1]; {
					i--
					base--
				}
				d += emitLiteral(dst[d:], src[nextEmit:base])

				// Extend forward
				candidate := s - repeat + 5
				s += 5
				for s <= sLimit {
					if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidate += 8
				}

				if true && nextS < s {
					// tiny bit better
					table[nextHash&tableMask] = uint32(nextS)
				}
				add := emitRepeat(dst[d:], repeat, s-base)
				// same as `add := emitCopy(dst[d:], repeat, s-base)` but skipping offset.
				d += add
				nextEmit = s
				if s >= sLimit {
					goto emitRemainder
				}

				if false {
					// usually not worth it:
					x := load64(src, s-2)
					hm2 := hash6(x, tableBits)
					hm1 := hash6(x>>8, tableBits)
					table[hm2&tableMask] = uint32(s - 2)
					table[hm1&tableMask] = uint32(s - 1)
					nextHash = hash6(x>>16, tableBits)
				} else {
					x := load64(src, s)
					nextHash = hash6(x, tableBits)
				}
				continue
			}

			if uint32(x) == load32(src, candidate) {
				break
			}
			s = nextS
		}

		// Extend backwards
		for candidate > 0 && s > nextEmit && src[candidate-1] == src[s-1] {
			candidate--
			s--
		}

		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.

		d += emitLiteral(dst[d:], src[nextEmit:s])

		// Call emitCopy, and then see if another emitCopy could be our next
		// move. Repeat until we find no match for the input immediately after
		// what was consumed by the last emitCopy call.
		//
		// If we exit this loop normally then we need to call emitLiteral next,
		// though we don't yet know how big the literal will be. We handle that
		// by proceeding to the next iteration of the main loop. We also can
		// exit this loop via goto if we get close to exhausting the input.
		for {
			// Invariant: we have a 4-byte match at s, and no need to emit any
			// literal bytes prior to s.
			base := s
			repeat = base - candidate

			// Extend the 4-byte match as long as possible.
			s += 4
			candidate += 4
			for s <= len(src)-8 {
				if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
					s += bits.TrailingZeros64(diff) >> 3
					break
				}
				s += 8
				candidate += 8
			}

			d += emitCopy(dst[d:], repeat, s-base)
			if false {
				// Validate match.
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}
			//fmt.Println("regular, len", s-base, "emitted", base-nextEmit, "offset:", repeat)

			nextEmit = s
			if s >= sLimit {
				goto emitRemainder
			}
			// We could immediately start working at s now, but to improve
			// compression we first update the hash table at s-1 and at s. If
			// another emitCopy is not our next move, also calculate nextHash
			// at s+1. At least on GOARCH=amd64, these three hash calculations
			// are faster as one load64 call (with some shifts) instead of
			// three load32 calls.
			x := load64(src, s-1)
			prevHash := hash6(x, tableBits)
			table[prevHash&tableMask] = uint32(s - 1)
			currHash := hash6(x>>8, tableBits)
			candidate = int(table[currHash&tableMask])
			table[currHash&tableMask] = uint32(s)
			if uint32(x>>8) != load32(src, candidate) {
				nextHash = hash6(x>>16, tableBits)
				s++
				break
			}
		}
	}

emitRemainder:
	if nextEmit < len(src) {
		d += emitLiteral(dst[d:], src[nextEmit:])
	}
	return d
}
