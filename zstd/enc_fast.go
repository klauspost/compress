// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"github.com/cespare/xxhash"
)

const (
	tableBits      = 15             // Bits used in the table
	tableSize      = 1 << tableBits // Size of the table
	tableMask      = tableSize - 1  // Mask for table indices. Redundant, but can eliminate bounds checks.
	maxMatchLength = 131074
)

type tableEntry struct {
	val    uint32
	offset int32
}

type fastEncoder struct {
	o encParams
	// cur is the offset at the start of Hist
	cur int32
	// maximum offset. Should be at least 2x block size.
	maxMatchOff int32
	hist        []byte
	crc         *xxhash.Digest
	table       [tableSize]tableEntry
	tmp         [8]byte
	blk         *blockEnc
	useRepeat   bool
}

// Encode mimmics functionality in zstd_fast.c but uses separate buffers for previous buffer and history.
// This should probably be refactored to a single buffer
func (e *fastEncoder) Encode(blk *blockEnc, src []byte) {
	const (
		inputMargin            = 8
		minNonLiteralBlockSize = 1 + 1 + inputMargin
	)

	// Protect against e.cur wraparound.
	for e.cur > (1<<30)+e.maxMatchOff {
		if len(e.hist) == 0 {
			for i := range e.table[:] {
				e.table[i] = tableEntry{}
			}
			e.cur = e.maxMatchOff
			break
		}
		// Shift down everything in the table that isn't already too far away.
		minOff := e.cur + int32(len(e.hist)) - e.maxMatchOff
		for i := range e.table[:] {
			v := e.table[i].offset
			if v < minOff {
				v = 0
			} else {
				v = v - e.cur + e.maxMatchOff
			}
			e.table[i].offset = v
		}
		e.cur = e.maxMatchOff
	}

	s := e.addBlock(src)
	blk.size = len(src)
	if len(src) < minNonLiteralBlockSize {
		blk.extraLits = len(src)
		blk.literals = blk.literals[:len(src)]
		copy(blk.literals, src)
		return
	}

	// Override src
	src = e.hist
	sLimit := int32(len(src)) - inputMargin
	// stepSize is the number of bytes to skip on every main loop iteration.
	// It should be >= 2.
	stepSize := int32(e.o.targetLength)
	if stepSize == 0 {
		stepSize++
	}
	stepSize++

	// TEMPLATE
	const hashLog = tableBits
	const mls = 6
	// seems global, but would be nice to tweak.
	const kSearchStrength = 8

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := s
	cv := load6432(src, s)
	// nextHash is the hash at s
	nextHash := hash6(cv, hashLog)

	// Relative offsets
	offset1 := int32(blk.recentOffsets[0])
	offset2 := int32(blk.recentOffsets[1])

	addLiterals := func(s *seq, until int32) {
		if until == nextEmit {
			return
		}
		blk.literals = append(blk.literals, src[nextEmit:until]...)
		s.litLen = uint32(until - nextEmit)
	}
	if debug {
		println("recent offsets:", blk.recentOffsets)
	}

encodeLoop:
	for {
		var t int32
		// We allow the encoder to optionally turn off repeat offsets across blocks
		canRepeat := e.useRepeat || len(blk.sequences) > 3

		// sMin is the smallest valid offset in src that a match can start.
		//var sMin int32

		for {
			if debug && canRepeat && offset1 == 0 {
				panic("offset0 was 0")
			}

			nextHash2 := hash6(cv>>8, hashLog) & tableMask

			//nextHash2 := hashLen(cv>>8, hashLog, mls) & tableMask
			if 8-mls < 0 {
				panic("hashlog doesn't leave 2 bytes")
			}
			nextHash = nextHash & tableMask
			candidate := e.table[nextHash]
			candidate2 := e.table[nextHash2]
			repIndex := s - offset1 + 2

			e.table[nextHash] = tableEntry{offset: s + e.cur, val: uint32(cv)}
			e.table[nextHash2] = tableEntry{offset: s + e.cur + 1, val: uint32(cv >> 8)}

			if canRepeat && repIndex >= 0 && load3232(src, repIndex) == uint32(cv>>16) {
				// Consider history as well.
				var seq seq
				lenght := 4 + e.matchlen(s+6, repIndex+4, src)

				seq.matchLen = uint32(lenght - zstdMinMatch)

				// We might be able to match backwards.
				// Extend as long as we can.
				start := s + 2
				// We end the search early, so we don't risk 0 literals
				// and have to do special offset treatment.
				startLimit := nextEmit + 1

				sMin := s - e.maxMatchOff
				if sMin < 0 {
					sMin = 0
				}
				for repIndex > sMin && start > startLimit && src[repIndex-1] == src[start-1] && seq.matchLen < maxMatchLength-zstdMinMatch {
					repIndex--
					start--
					seq.matchLen++
				}
				addLiterals(&seq, start)

				// rep 0
				seq.offset = 1
				if debugSequences {
					println("repeat sequence", seq, "next s:", s)
				}
				blk.sequences = append(blk.sequences, seq)
				s += lenght + 2
				nextEmit = s
				if s >= sLimit {
					if debug {
						println("repeat ended", s, lenght)

					}
					break encodeLoop
				}
				cv = load6432(src, s)
				//nextHash = hashLen(cv, hashLog, mls)
				nextHash = hash6(cv, hashLog)
				continue
			}
			coffset0 := s - (candidate.offset - e.cur)
			coffset1 := s - (candidate2.offset - e.cur) + 1
			if coffset0 < e.maxMatchOff && uint32(cv) == candidate.val {
				// found a regular match
				t = candidate.offset - e.cur
				if debug && s <= t {
					panic("s <= t")
				}
				if debug && s-t > e.maxMatchOff {
					panic("s - t >e.maxMatchOff")
				}
				break
			}

			if coffset1 < e.maxMatchOff && uint32(cv>>8) == candidate2.val {
				// found a regular match
				t = candidate2.offset - e.cur
				s++
				if debug && s <= t {
					panic("s <= t")
				}
				if debug && s-t > e.maxMatchOff {
					panic("s - t >e.maxMatchOff")
				}
				if debug && t < 0 {
					panic("t<0")
				}
				break
			}
			s += stepSize + ((s - nextEmit) >> (kSearchStrength - 1))
			if s >= sLimit {
				break encodeLoop
			}
			cv = load6432(src, s)
			//nextHash = hashLen(cv, hashLog, mls)
			nextHash = hash6(cv, hashLog)
		}
		offset2 = offset1
		offset1 = s - t

		if debug && s <= t {
			panic("s <= t")
		}

		if debug && canRepeat && int(offset1) > len(src) {
			panic("invalid offset")
		}

		var seq seq
		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.
		seq.litLen = uint32(s - nextEmit)

		// Extend the 4-byte match as long as possible.
		l := e.matchlen(s+4, t+4, src)

		// Extend backwards
		sMin := s - e.maxMatchOff
		if sMin < 0 {
			sMin = 0
		}
		for t > sMin && seq.litLen > 0 && src[t-1] == src[s-1] && l < maxMatchLength {
			s--
			t--
			l++
			seq.litLen--
		}
		l += 4
		seq.matchLen = uint32(l - zstdMinMatch)
		if seq.litLen > 0 {
			blk.literals = append(blk.literals, src[nextEmit:s]...)
		}
		// Don't use repeat offsets
		seq.offset = uint32(s-t) + 3
		s += l
		if debugSequences {
			println("sequence", seq, "next s:", s)
		}
		blk.sequences = append(blk.sequences, seq)
		nextEmit = s
		if s >= sLimit {
			break encodeLoop
		}
		cv = load6432(src, s)
		//		nextHash = hashLen(cv, hashLog, mls)
		nextHash = hash6(cv, hashLog)

		// Check offset 2
		if o2 := s - offset2; canRepeat && o2 > 0 && load3232(src, o2) == uint32(cv) {
			// We have at least 4 byte match.
			// No need to check backwards. We come straight from a match
			l := 4 + e.matchlen(s+4, o2+4, src)
			// Store this, since we have it.
			e.table[nextHash&tableMask] = tableEntry{offset: s + e.cur, val: uint32(cv)}
			seq.matchLen = uint32(l) - zstdMinMatch
			seq.litLen = 0
			// Since litlen is always 0, this is offset 1.
			seq.offset = 1
			s += l
			nextEmit = s
			if debugSequences {
				println("sequence", seq, "next s:", s)
			}
			blk.sequences = append(blk.sequences, seq)

			// Swap offset 1 and 2.
			offset1, offset2 = offset2, offset1
			if s >= sLimit {
				break encodeLoop
			}
			// Prepare next loop.
			cv = load6432(src, s)
			nextHash = hash6(cv, hashLog)
		}
	}

	if int(nextEmit) < len(src) {
		blk.literals = append(blk.literals, src[nextEmit:]...)
		blk.extraLits = len(src) - int(nextEmit)
	}
	blk.recentOffsets[0] = uint32(offset1)
	blk.recentOffsets[1] = uint32(offset2)
	if debug {
		println("returning, recent offsets:", blk.recentOffsets, "extra literals:", blk.extraLits)
	}
}

func (e *fastEncoder) addBlock(src []byte) int32 {
	// check if we have space already
	if len(e.hist)+len(src) > cap(e.hist) {
		if cap(e.hist) == 0 {
			l := e.maxMatchOff * 2
			// Make it at least 1MB.
			if l < 1<<20 {
				l = 1 << 20
			}
			e.hist = make([]byte, 0, l)
		} else {
			if cap(e.hist) < int(e.maxMatchOff*2) {
				panic("unexpected buffer size")
			}
			// Move down
			offset := int32(len(e.hist)) - e.maxMatchOff
			copy(e.hist[0:e.maxMatchOff], e.hist[offset:])
			e.cur += offset
			e.hist = e.hist[:e.maxMatchOff]
		}
	}
	s := int32(len(e.hist))
	e.hist = append(e.hist, src...)
	return s
}

// useBlock will replace the block with the provided one,
// but transfer recent offsets from the previous.
func (e *fastEncoder) useBlock(enc *blockEnc) {
	enc.reset(e.blk)
	e.blk = enc
}

func (e *fastEncoder) matchlen(s, t int32, src []byte) int32 {
	if debug {
		if s < 0 {
			panic("s<0")
		}
		if t < 0 {
			panic("t<0")
		}
		if s-t > e.maxMatchOff {
			panic(s - t)
		}
	}
	s1 := int(s) + maxMatchLength - 4
	if s1 > len(src) {
		s1 = len(src)
	}

	// Extend the match to be as long as possible.
	return int32(matchLen(src[s:s1], src[t:]))
}

// Reset the encoding table.
func (e *fastEncoder) Reset() {
	if e.blk == nil {
		e.blk = &blockEnc{}
		e.blk.init()
	}
	e.blk.initNewEncode()
	if e.crc == nil {
		e.crc = xxhash.New()
	} else {
		e.crc.Reset()
	}
	if cap(e.hist) < int(e.maxMatchOff*2) {
		l := e.maxMatchOff * 2
		// Make it at least 1MB.
		if l < 1<<20 {
			l = 1 << 20
		}
		e.hist = make([]byte, 0, l)
	}
	// We offset current position so everything will be out of reach
	e.cur += e.maxMatchOff + int32(len(e.hist))
	e.hist = e.hist[:0]
}
