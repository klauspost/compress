// Copyright 2016 The Snappy-Go Authors. All rights reserved.
// Copyright (c) 2019 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
)

func load32(b []byte, i int) uint32 {
	return binary.LittleEndian.Uint32(b[i:])
}

func load64(b []byte, i int) uint64 {
	return binary.LittleEndian.Uint64(b[i:])
}

// hash6 returns the hash of the lowest 6 bytes of u to fit in a hash table with h bits.
// Preferably h should be a constant and should always be <64.
func hash6(u uint64, h uint8) uint32 {
	const prime6bytes = 227718039650203
	return uint32(((u << (64 - 48)) * prime6bytes) >> ((64 - h) & 63))
}

func encodeGo(dst, src []byte) []byte {
	if n := MaxEncodedLen(len(src)); n < 0 {
		panic(ErrTooLarge)
	} else if len(dst) < n {
		dst = make([]byte, n)
	}

	// The block starts with the varint-encoded length of the decompressed bytes.
	d := binary.PutUvarint(dst, uint64(len(src)))

	if len(src) == 0 {
		return dst[:d]
	}
	if len(src) < minNonLiteralBlockSize {
		d += emitLiteral(dst[d:], src)
		return dst[:d]
	}
	n := encodeBlockGo(dst[d:], src)
	if n > 0 {
		d += n
		return dst[:d]
	}
	// Not compressible
	d += emitLiteral(dst[d:], src)
	return dst[:d]
}

// encodeBlockGo encodes a non-empty src to a guaranteed-large-enough dst. It
// assumes that the varint-encoded length of the decompressed bytes has already
// been written.
//
// It also assumes that:
//
//	len(dst) >= MaxEncodedLen(len(src)) &&
//	minNonLiteralBlockSize <= len(src) && len(src) <= maxBlockSize
func encodeBlockGo(dst, src []byte) (d int) {
	// Initialize the hash table.
	const (
		tableBits    = 14
		maxTableSize = 1 << tableBits

		debug = false
	)

	var table [maxTableSize]uint32

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := len(src) - inputMargin

	// Bail if we can't compress to at least this.
	dstLimit := len(src) - len(src)>>5 - 5

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := 0

	// The encoded form must start with a literal, as there are no previous
	// bytes to copy, so we start looking for hash matches at s == 1.
	s := 1
	cv := load64(src, s)

	// We search for a repeat at -1, but don't output repeats when nextEmit == 0
	repeat := 1

	for {
		candidate := 0
		for {
			// Next src position to check
			nextS := s + (s-nextEmit)>>6 + 4
			if nextS > sLimit {
				goto emitRemainder
			}
			hash0 := hash6(cv, tableBits)
			hash1 := hash6(cv>>8, tableBits)
			candidate = int(table[hash0])
			candidate2 := int(table[hash1])
			table[hash0] = uint32(s)
			table[hash1] = uint32(s + 1)
			hash2 := hash6(cv>>16, tableBits)

			// Check repeat at offset checkRep.
			const checkRep = 1
			if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				base := s + checkRep
				// Extend back
				for i := base - repeat; base > nextEmit && i > 0 && src[i-1] == src[base-1]; {
					i--
					base--
				}
				d += emitLiteral(dst[d:], src[nextEmit:base])

				// Extend forward
				candidate := s - repeat + 4 + checkRep
				s += 4 + checkRep
				for s <= sLimit {
					if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidate += 8
				}
				if debug {
					// Validate match.
					if s <= candidate {
						panic("s <= candidate")
					}
					a := src[base:s]
					b := src[base-repeat : base-repeat+(s-base)]
					if !bytes.Equal(a, b) {
						panic("mismatch")
					}
				}
				if nextEmit > 0 {
					// same as `add := emitCopy(dst[d:], repeat, s-base)` but skips storing offset.
					d += emitRepeat(dst[d:], repeat, s-base)
				} else {
					// First match, cannot be repeat.
					d += emitCopy(dst[d:], repeat, s-base)
				}
				nextEmit = s
				if s >= sLimit {
					goto emitRemainder
				}

				cv = load64(src, s)
				continue
			}

			if uint32(cv) == load32(src, candidate) {
				break
			}
			candidate = int(table[hash2])
			if uint32(cv>>8) == load32(src, candidate2) {
				table[hash2] = uint32(s + 2)
				candidate = candidate2
				s++
				break
			}
			table[hash2] = uint32(s + 2)
			if uint32(cv>>16) == load32(src, candidate) {
				s += 2
				break
			}

			cv = load64(src, nextS)
			s = nextS
		}

		// Extend backwards.
		// The top bytes will be rechecked to get the full match.
		for candidate > 0 && s > nextEmit && src[candidate-1] == src[s-1] {
			candidate--
			s--
		}

		// Bail if we exceed the maximum size.
		if d+(s-nextEmit) > dstLimit {
			return 0
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
			if debug {
				// Validate match.
				if s <= candidate {
					panic("s <= candidate")
				}
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}

			nextEmit = s
			if s >= sLimit {
				goto emitRemainder
			}

			if d > dstLimit {
				// Do we have space for more, if not bail.
				return 0
			}
			// Check for an immediate match, otherwise start search at s+1
			x := load64(src, s-2)
			m2Hash := hash6(x, tableBits)
			currHash := hash6(x>>16, tableBits)
			candidate = int(table[currHash])
			table[m2Hash] = uint32(s - 2)
			table[currHash] = uint32(s)
			if debug && s == candidate {
				panic("s == candidate")
			}
			if uint32(x>>16) != load32(src, candidate) {
				cv = load64(src, s+1)
				s++
				break
			}
		}
	}

emitRemainder:
	if nextEmit < len(src) {
		// Bail if we exceed the maximum size.
		if d+len(src)-nextEmit > dstLimit {
			return 0
		}
		d += emitLiteral(dst[d:], src[nextEmit:])
	}
	return d
}

func encodeBlockSnappyGo(dst, src []byte) (d int) {
	// Initialize the hash table.
	const (
		tableBits    = 14
		maxTableSize = 1 << tableBits
	)

	var table [maxTableSize]uint32

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := len(src) - inputMargin

	// Bail if we can't compress to at least this.
	dstLimit := len(src) - len(src)>>5 - 5

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := 0

	// The encoded form must start with a literal, as there are no previous
	// bytes to copy, so we start looking for hash matches at s == 1.
	s := 1
	cv := load64(src, s)

	// We search for a repeat at -1, but don't output repeats when nextEmit == 0
	repeat := 1

	for {
		candidate := 0
		for {
			// Next src position to check
			nextS := s + (s-nextEmit)>>6 + 4
			if nextS > sLimit {
				goto emitRemainder
			}
			hash0 := hash6(cv, tableBits)
			hash1 := hash6(cv>>8, tableBits)
			candidate = int(table[hash0])
			candidate2 := int(table[hash1])
			table[hash0] = uint32(s)
			table[hash1] = uint32(s + 1)
			hash2 := hash6(cv>>16, tableBits)

			// Check repeat at offset checkRep.
			const checkRep = 1
			if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				base := s + checkRep
				// Extend back
				for i := base - repeat; base > nextEmit && i > 0 && src[i-1] == src[base-1]; {
					i--
					base--
				}
				d += emitLiteral(dst[d:], src[nextEmit:base])

				// Extend forward
				candidate := s - repeat + 4 + checkRep
				s += 4 + checkRep
				for s <= sLimit {
					if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidate += 8
				}

				d += emitCopyNoRepeat(dst[d:], repeat, s-base)
				nextEmit = s
				if s >= sLimit {
					goto emitRemainder
				}

				cv = load64(src, s)
				continue
			}

			if uint32(cv) == load32(src, candidate) {
				break
			}
			candidate = int(table[hash2])
			if uint32(cv>>8) == load32(src, candidate2) {
				table[hash2] = uint32(s + 2)
				candidate = candidate2
				s++
				break
			}
			table[hash2] = uint32(s + 2)
			if uint32(cv>>16) == load32(src, candidate) {
				s += 2
				break
			}

			cv = load64(src, nextS)
			s = nextS
		}

		// Extend backwards
		for candidate > 0 && s > nextEmit && src[candidate-1] == src[s-1] {
			candidate--
			s--
		}

		// Bail if we exceed the maximum size.
		if d+(s-nextEmit) > dstLimit {
			return 0
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

			d += emitCopyNoRepeat(dst[d:], repeat, s-base)
			if false {
				// Validate match.
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}

			nextEmit = s
			if s >= sLimit {
				goto emitRemainder
			}

			if d > dstLimit {
				// Do we have space for more, if not bail.
				return 0
			}
			// Check for an immediate match, otherwise start search at s+1
			x := load64(src, s-2)
			m2Hash := hash6(x, tableBits)
			currHash := hash6(x>>16, tableBits)
			candidate = int(table[currHash])
			table[m2Hash] = uint32(s - 2)
			table[currHash] = uint32(s)
			if uint32(x>>16) != load32(src, candidate) {
				cv = load64(src, s+1)
				s++
				break
			}
		}
	}

emitRemainder:
	if nextEmit < len(src) {
		// Bail if we exceed the maximum size.
		if d+len(src)-nextEmit > dstLimit {
			return 0
		}
		d += emitLiteral(dst[d:], src[nextEmit:])
	}
	return d
}

// encodeBlockGo encodes a non-empty src to a guaranteed-large-enough dst. It
// assumes that the varint-encoded length of the decompressed bytes has already
// been written.
//
// It also assumes that:
//
//	len(dst) >= MaxEncodedLen(len(src)) &&
//	minNonLiteralBlockSize <= len(src) && len(src) <= maxBlockSize
func encodeBlockDictGo(dst, src []byte, dict *Dict) (d int) {
	// Initialize the hash table.
	const (
		tableBits    = 14
		maxTableSize = 1 << tableBits

		debug = false
	)
	dict.initFast()

	var table [maxTableSize]uint32

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := len(src) - inputMargin
	if sLimit > 65535 {
		sLimit = 65535
	}

	// Bail if we can't compress to at least this.
	dstLimit := len(src) - len(src)>>5 - 5

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := 0

	// The encoded form can start with a dict entry (copy or repeat).
	s := 0

	// Convert dict repeat to offset
	repeat := len(dict.dict) - dict.repeat
	cv := load64(src, 0)

	// While in dict
searchDict:
	for {
		// Next src position to check
		nextS := s + (s-nextEmit)>>6 + 4
		hash0 := hash6(cv, tableBits)
		hash1 := hash6(cv>>8, tableBits)
		if nextS > sLimit {
			fmt.Println("slimit reached", s, nextS)
			break searchDict
		}
		candidateDict := int(dict.fastTable[hash0])
		candidateDict2 := int(dict.fastTable[hash0])
		candidate2 := int(table[hash1])
		candidate := int(table[hash0])
		table[hash0] = uint32(s)
		table[hash1] = uint32(s + 1)
		hash2 := hash6(cv>>16, tableBits)

		// Check repeat at offset checkRep.
		const checkRep = 1

		if repeat > s {
			candidate := len(dict.dict) - repeat + s
			if repeat-s >= 4 && uint32(cv) == load32(dict.dict, candidate) {
				// Extend back
				base := s
				for i := candidate; base > nextEmit && i > 0 && dict.dict[i-1] == src[base-1]; {
					i--
					base--
				}
				d += emitLiteral(dst[d:], src[nextEmit:s])
				s += 4
				candidate += 4
				for candidate < len(dict.dict)-8 {
					if diff := load64(src, s) ^ load64(dict.dict, candidate); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidate += 8
				}
				d += emitRepeat(dst[d:], repeat, s-base)
				fmt.Println("emitted dict repeat", s-base, repeat)
				cv = load64(src, s)
				nextEmit = s
				continue
			}
		} else if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
			base := s + checkRep
			// Extend back
			for i := base - repeat; base > nextEmit && i > 0 && src[i-1] == src[base-1]; {
				i--
				base--
			}
			d += emitLiteral(dst[d:], src[nextEmit:base])

			// Extend forward
			candidate := s - repeat + 4 + checkRep
			s += 4 + checkRep
			for s <= sLimit {
				if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
					s += bits.TrailingZeros64(diff) >> 3
					break
				}
				s += 8
				candidate += 8
			}
			if debug {
				// Validate match.
				if s <= candidate {
					panic("s <= candidate")
				}
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}
			if nextEmit > 0 {
				// same as `add := emitCopy(dst[d:], repeat, s-base)` but skips storing offset.
				d += emitRepeat(dst[d:], repeat, s-base)
			} else {
				// First match, cannot be repeat.
				d += emitCopy(dst[d:], repeat, s-base)
			}
			nextEmit = s
			if s >= sLimit {
				break searchDict
			}
			fmt.Println("emitted reg repeat", s-base)

			cv = load64(src, s)
			continue searchDict
		}
		if s == 0 {
			cv = load64(src, nextS)
			s = nextS
		}
		// Start with table. These matches will always be closer.
		if uint32(cv) == load32(src, candidate) {
			goto emitMatch
		}
		candidate = int(table[hash2])
		if uint32(cv>>8) == load32(src, candidate2) {
			table[hash2] = uint32(s + 2)
			candidate = candidate2
			s++
			goto emitMatch
		}

		// Check dict...
		if uint32(cv) == load32(dict.dict, candidateDict) {
			if debug {
				fmt.Println("dict s0")
			}
			table[hash2] = uint32(s + 2)
			goto emitDict
		}
		candidateDict = int(dict.fastTable[hash2])
		if uint32(cv>>8) == load32(dict.dict, candidateDict2) {
			if debug {
				fmt.Println("dict s1")
			}
			table[hash2] = uint32(s + 2)
			candidateDict = candidateDict2
			s++
			goto emitDict
		}

		table[hash2] = uint32(s + 2)
		if uint32(cv>>16) == load32(src, candidate) {
			s += 2
			goto emitMatch
		}
		if uint32(cv>>16) == load32(dict.dict, candidateDict) {
			if debug {
				fmt.Println("dict s1")
			}
			s += 2
			goto emitDict
		}

		cv = load64(src, nextS)
		s = nextS
		continue searchDict

	emitDict:
		{
			if debug {
				if load32(dict.dict, candidateDict) != load32(src, s) {
					panic("dict emit mismatch")
				}
			}
			// Extend backwards.
			// The top bytes will be rechecked to get the full match.
			for candidateDict > 0 && s > nextEmit && dict.dict[candidateDict-1] == src[s-1] {
				candidateDict--
				s--
			}

			// Bail if we exceed the maximum size.
			if d+(s-nextEmit) > dstLimit {
				return 0
			}

			// A 4-byte match has been found. We'll later see if more than 4 bytes
			// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
			// them as literal bytes.

			d += emitLiteral(dst[d:], src[nextEmit:s])

			{
				// Invariant: we have a 4-byte match at s, and no need to emit any
				// literal bytes prior to s.
				base := s
				repeat = s + (len(dict.dict)) - candidateDict

				// Extend the 4-byte match as long as possible.
				s += 4
				candidateDict += 4
				for s <= len(src)-8 && len(dict.dict)-candidateDict >= 8 {
					if diff := load64(src, s) ^ load64(dict.dict, candidateDict); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidateDict += 8
				}

				d += emitCopy(dst[d:], repeat, s-base)
				if debug {
					// Validate match.
					if s <= candidate {
						panic("s <= candidate")
					}
					a := src[base:s]
					b := src[base-repeat : base-repeat+(s-base)]
					if !bytes.Equal(a, b) {
						panic("mismatch")
					}
				}
				fmt.Println("emitted dict copy", s-base)

				nextEmit = s
				if s >= sLimit {
					break searchDict
				}

				if d > dstLimit {
					// Do we have space for more, if not bail.
					return 0
				}

				// Index and continue loop to try new candidate.
				x := load64(src, s-2)
				m2Hash := hash6(x, tableBits)
				currHash := hash6(x>>8, tableBits)
				candidate = int(table[currHash])
				table[m2Hash] = uint32(s - 2)
				table[currHash] = uint32(s - 1)
				cv = load64(src, s)
			}
			continue
		}
	emitMatch:

		// Extend backwards.
		// The top bytes will be rechecked to get the full match.
		for candidate > 0 && s > nextEmit && src[candidate-1] == src[s-1] {
			candidate--
			s--
		}

		// Bail if we exceed the maximum size.
		if d+(s-nextEmit) > dstLimit {
			return 0
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
			if debug {
				// Validate match.
				if s <= candidate {
					panic("s <= candidate")
				}
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}
			fmt.Println("emitted src copy", s-base)

			nextEmit = s
			if s >= sLimit {
				break searchDict
			}

			if d > dstLimit {
				// Do we have space for more, if not bail.
				return 0
			}
			// Check for an immediate match, otherwise start search at s+1
			x := load64(src, s-2)
			m2Hash := hash6(x, tableBits)
			currHash := hash6(x>>16, tableBits)
			candidate = int(table[currHash])
			table[m2Hash] = uint32(s - 2)
			table[currHash] = uint32(s)
			if debug && s == candidate {
				panic("s == candidate")
			}
			if uint32(x>>16) != load32(src, candidate) {
				cv = load64(src, s+1)
				s++
				break
			}
		}
	}

	// Search without dict:
	if repeat > s {
		repeat = 0
	}

	// No more dict
	sLimit = len(src) - inputMargin
	fmt.Println("now", s, "->", sLimit, "out:", d, "left:", len(src)-s, "nextemit:", nextEmit, "dstLimit:", dstLimit)
	for {
		candidate := 0
		for {
			// Next src position to check
			nextS := s + (s-nextEmit)>>6 + 4
			if nextS > sLimit {
				goto emitRemainder
			}
			hash0 := hash6(cv, tableBits)
			hash1 := hash6(cv>>8, tableBits)
			candidate = int(table[hash0])
			candidate2 := int(table[hash1])
			table[hash0] = uint32(s)
			table[hash1] = uint32(s + 1)
			hash2 := hash6(cv>>16, tableBits)

			// Check repeat at offset checkRep.
			const checkRep = 1
			if repeat > 0 && uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				base := s + checkRep
				// Extend back
				for i := base - repeat; base > nextEmit && i > 0 && src[i-1] == src[base-1]; {
					i--
					base--
				}
				d += emitLiteral(dst[d:], src[nextEmit:base])

				// Extend forward
				candidate := s - repeat + 4 + checkRep
				s += 4 + checkRep
				for s <= sLimit {
					if diff := load64(src, s) ^ load64(src, candidate); diff != 0 {
						s += bits.TrailingZeros64(diff) >> 3
						break
					}
					s += 8
					candidate += 8
				}
				if debug {
					// Validate match.
					if s <= candidate {
						panic("s <= candidate")
					}
					a := src[base:s]
					b := src[base-repeat : base-repeat+(s-base)]
					if !bytes.Equal(a, b) {
						panic("mismatch")
					}
				}
				if nextEmit > 0 {
					// same as `add := emitCopy(dst[d:], repeat, s-base)` but skips storing offset.
					d += emitRepeat(dst[d:], repeat, s-base)
				} else {
					// First match, cannot be repeat.
					d += emitCopy(dst[d:], repeat, s-base)
				}
				fmt.Println("emitted src repeat", s-base, repeat)
				nextEmit = s
				if s >= sLimit {
					goto emitRemainder
				}

				cv = load64(src, s)
				continue
			}

			if uint32(cv) == load32(src, candidate) {
				break
			}
			candidate = int(table[hash2])
			if uint32(cv>>8) == load32(src, candidate2) {
				table[hash2] = uint32(s + 2)
				candidate = candidate2
				s++
				break
			}
			table[hash2] = uint32(s + 2)
			if uint32(cv>>16) == load32(src, candidate) {
				s += 2
				break
			}

			cv = load64(src, nextS)
			s = nextS
		}

		// Extend backwards.
		// The top bytes will be rechecked to get the full match.
		for candidate > 0 && s > nextEmit && src[candidate-1] == src[s-1] {
			candidate--
			s--
		}

		// Bail if we exceed the maximum size.
		if d+(s-nextEmit) > dstLimit {
			return 0
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
			if debug {
				// Validate match.
				if s <= candidate {
					panic("s <= candidate")
				}
				a := src[base:s]
				b := src[base-repeat : base-repeat+(s-base)]
				if !bytes.Equal(a, b) {
					panic("mismatch")
				}
			}
			fmt.Println("emitted src copy", s-base)

			nextEmit = s
			if s >= sLimit {
				goto emitRemainder
			}

			if d > dstLimit {
				// Do we have space for more, if not bail.
				return 0
			}
			// Check for an immediate match, otherwise start search at s+1
			x := load64(src, s-2)
			m2Hash := hash6(x, tableBits)
			currHash := hash6(x>>16, tableBits)
			candidate = int(table[currHash])
			table[m2Hash] = uint32(s - 2)
			table[currHash] = uint32(s)
			if debug && s == candidate {
				panic("s == candidate")
			}
			if uint32(x>>16) != load32(src, candidate) {
				cv = load64(src, s+1)
				s++
				break
			}
		}
	}

emitRemainder:
	if nextEmit < len(src) {
		// Bail if we exceed the maximum size.
		if d+len(src)-nextEmit > dstLimit {
			return 0
		}
		d += emitLiteral(dst[d:], src[nextEmit:])
	}
	return d
}
