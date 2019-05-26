package zstd

const (
	tableBits         = 15                // Bits used in the table
	tableSize         = 1 << tableBits    // Size of the table
	tableMask         = tableSize - 1     // Mask for table indices. Redundant, but can eliminate bounds checks.
	tableShift        = 32 - tableBits    // Right-shift to get the tableBits most significant bits of a uint32.
	maxMatchOffset    = maxStoreBlockSize // The largest match offset
	maxStoreBlockSize = 1 << 16
	maxMatchLength    = (1 << 16) - 1
)

func hashFn(u uint32) uint32 {
	return (u * 2654435761) >> tableShift
}

type tableEntry struct {
	val    uint32
	offset int32
}

func load3232(b []byte, i int32) uint32 {
	b = b[i : i+4 : len(b)] // Help the compiler eliminate bounds checks on the next line.
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// cmp32 will compare a uint32 with 4 bytes at offset i in b.
func cmp32(b []byte, i int32, want uint32) bool {
	// Help the compiler eliminate bounds checks on the next lines.
	b = b[i : i+4]
	return b[3] == uint8(want>>24) &&
		b[2] == uint8(want>>16) &&
		b[1] == uint8(want>>8) &&
		b[0] == uint8(want)
}

func load6432(b []byte, i int32) uint64 {
	b = b[i : i+8 : len(b)] // Help the compiler eliminate bounds checks on the next line.
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

type simpleEncoder struct {
	o     encParams
	prev  []byte
	cur   int32
	table [tableSize]tableEntry
}

// Encode uses a simple  algorithm, mostly lifted from deflate of finding
// of matching across blocks giving better compression at a small slowdown.
func (e *simpleEncoder) Encode(blk *blockEnc, src []byte) {
	const (
		inputMargin            = 12 - 1
		minNonLiteralBlockSize = 1 + 1 + inputMargin
	)

	// Protect against e.cur wraparound.
	if e.cur > 1<<30 {
		for i := range e.table[:] {
			e.table[i] = tableEntry{}
		}
		e.cur = maxStoreBlockSize
	}
	blk.size = len(src)
	// This check isn't in the Snappy implementation, but there, the caller
	// instead of the callee handles this case.
	if len(src) < minNonLiteralBlockSize {
		// We do not fill the token table.
		// This will be picked up by caller.
		blk.extraLits = len(src)
		copy(blk.literals[:len(src)], src)
		e.cur += maxMatchOffset
		e.prev = src
		return
	}

	// Based on the entropy of the input, calculate a minimum length we want.
	// This is in addition to the 4 bytes we already matched
	//minLen := int32(5 - compress.SnannonEntropyBits(src)/len(src))
	minLen := int32(0)
	//fmt.Println("Entropy:", float64(compress.SnannonEntropyBits(src))/float64(len(src)), "bits per symbol. Min len:", minLen)

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := int32(len(src) - inputMargin)

	// nextEmit is where in src the next emitLiteral should start from.
	nextEmit := int32(0)
	s := int32(0)
	cv := load3232(src, s)
	nextHash := hashFn(cv)

	for {
		// Copied from the C++ snappy implementation:
		//
		// Heuristic match skipping: If 32 bytes are scanned with no matches
		// found, start looking only at every other byte. If 32 more bytes are
		// scanned (or skipped), look at every third byte, etc.. When a match
		// is found, immediately go back to looking at every byte. This is a
		// small loss (~5% performance, ~0.1% density) for compressible data
		// due to more bookkeeping, but for non-compressible data (such as
		// JPEG) it's a huge win since the compressor quickly "realizes" the
		// data is incompressible and doesn't bother looking for matches
		// everywhere.
		//
		// The "skip" variable keeps track of how many bytes there are since
		// the last match; dividing it by 32 (ie. right-shifting by five) gives
		// the number of bytes to move ahead for each iteration.
		const skipLog2 = 4
		skip := int32(1 << skipLog2)

		nextS := s
		var candidate tableEntry
		for {
			s = nextS
			bytesBetweenHashLookups := skip >> skipLog2
			nextS = s + bytesBetweenHashLookups
			skip += bytesBetweenHashLookups
			if nextS > sLimit {
				goto emitRemainder
			}
			candidate = e.table[nextHash&tableMask]

			// Load enough for 3 match attempts:
			// Loading: [76543210], we attempt to match [3210], [5432] and [6543] will be ready for next loop.
			// On the last attempt we skip one more, so we increment by 3 (+skip) on evey loop
			now := load6432(src, nextS)
			e.table[nextHash&tableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hashFn(uint32(now))

			offset := s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				break
			}

			// Out of range or not matched.
			// Skip 1 byte and try again.
			cv = uint32(now)
			s = nextS
			// Prepare next
			now >>= 16
			nextS += 2
			candidate = e.table[nextHash&tableMask]
			nextHash = hashFn(uint32(now))
			offset = s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				break
			}

			// Out of range or not matched.
			// Skip no bytes before trying.
			cv = uint32(now)
			s = nextS
			// Prepare next
			now >>= 8
			nextS += 1
			candidate = e.table[nextHash&tableMask]
			nextHash = hashFn(uint32(now))
			offset = s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				break
			}
			// Out of range or not matched.
			cv = uint32(now)
		}
		var seq seq
		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.
		seq.litLen = uint32(s - nextEmit)

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

			// Extend the 4-byte match as long as possible.
			t := candidate.offset - e.cur
			l := e.matchlen(s+4, t+4, src)

			// Extend backwards
			for t > 0 && seq.litLen > 0 && src[t-1] == src[s-1] {
				s--
				t--
				l++
				seq.litLen--
			}
			for t <= 0 && seq.litLen > 0 && s > 0 {
				off := int32(len(e.prev)) + t - 1
				if off > 0 && e.prev[off] == src[s-1] {
					s--
					t--
					l++
					seq.litLen--
					continue
				}
				break
			}

			// Short matches are often not too good. Extending them may be preferable.
			if false && l < minLen {
				s += 2
				cv = load3232(src, s)
				nextHash = hashFn(cv)
				break
			}

			seq.matchLen = uint32(l + 4 - zstdMinMatch)
			if seq.litLen > 0 {
				blk.literals = append(blk.literals, src[nextEmit:s]...)
			}

			// Don't use repeat offsets
			seq.offset = uint32(s-t) + 3
			//seq.offset = blk.matchOffset(uint32(s-t), seq.litLen)

			s += 4
			blk.sequences = append(blk.sequences, seq)
			seq.litLen = 0
			// Store every second hash in-between, but offset by 1.
			for i := s - 2; i < s+l-7; i += 5 {
				x := load6432(src, i)
				prevHash := hashFn(uint32(x))
				e.table[prevHash&tableMask] = tableEntry{offset: e.cur + i, val: uint32(x)}
				// Skip one
				x >>= 16
				prevHash = hashFn(uint32(x))
				e.table[prevHash&tableMask] = tableEntry{offset: e.cur + i + 2, val: uint32(x)}
				// Skip one
				x >>= 16
				prevHash = hashFn(uint32(x))
				e.table[prevHash&tableMask] = tableEntry{offset: e.cur + i + 4, val: uint32(x)}
			}
			s += l
			nextEmit = s
			if s >= sLimit {
				t += l
				// Index first pair after match end.
				if int(t+4) < len(src) && t > 0 {
					cv := load3232(src, t)
					e.table[hashFn(cv)&tableMask] = tableEntry{offset: t + e.cur, val: cv}
				}
				goto emitRemainder
			}

			// We could immediately start working at s now, but to improve
			// compression we first update the hashFn table at s-1 and at s. If
			// another emitCopy is not our next move, also calculate nextHash
			// at s+1. At least on GOARCH=amd64, these three hashFn calculations
			// are faster as one load64 call (with some shifts) instead of
			// three load32 calls.
			x := load6432(src, s-3)
			prevHash := hashFn(uint32(x))
			e.table[prevHash&tableMask] = tableEntry{offset: e.cur + s - 3, val: uint32(x)}
			x >>= 16
			// Skip one
			prevHash = hashFn(uint32(x))
			e.table[prevHash&tableMask] = tableEntry{offset: e.cur + s - 1, val: uint32(x)}
			x >>= 8
			currHash := hashFn(uint32(x))
			candidate = e.table[currHash&tableMask]
			e.table[currHash&tableMask] = tableEntry{offset: e.cur + s, val: uint32(x)}

			offset := s - (candidate.offset - e.cur)
			if offset > maxMatchOffset || uint32(x) != candidate.val {
				cv = uint32(x >> 8)
				nextHash = hashFn(cv)
				s++
				break
			}
		}
	}

emitRemainder:
	if int(nextEmit) < len(src) {
		//emitLiteral(dst, src[nextEmit:])
		blk.literals = append(blk.literals, src[nextEmit:]...)
		blk.extraLits = len(src) - int(nextEmit)
	}
	e.cur += int32(len(src))
	e.prev = src
}

// Encode uses a simple  algorithm, mostly lifted from deflate of finding
// of matching across blocks giving better compression at a small slowdown.
func (e *simpleEncoder) EncodeFast(blk *blockEnc, src []byte) {
	const (
		inputMargin            = 8
		minNonLiteralBlockSize = 1 + 1 + inputMargin
	)

	// Protect against e.cur wraparound.
	if e.cur > 1<<30 {
		for i := range e.table[:] {
			e.table[i] = tableEntry{}
		}
		e.cur = maxStoreBlockSize
	}
	blk.size = len(src)
	// This check isn't in the Snappy implementation, but there, the caller
	// instead of the callee handles this case.
	if len(src) < minNonLiteralBlockSize {
		// We do not fill the token table.
		// This will be picked up by caller.
		blk.extraLits = len(src)
		copy(blk.literals[:len(src)], src)
		e.cur += maxMatchOffset
		e.prev = src
		return
	}

	sLimit := int32(len(src) - inputMargin)
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
	nextEmit := int32(0)
	s := int32(0)
	cv := load6432(src, s)
	// nextHash is the hash at s
	//nextHash := hashLen(cv, hashLog, mls)
	nextHash := hash6(cv, hashLog)

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
		for {
			if debug && offset1 == 0 {
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

			if e.cmp32Hist(uint32(cv>>16), repIndex, src) {
				// Consider history as well.
				var seq seq
				lenght := 4 + e.matchlen(s+6, repIndex+4, src)

				// We might be able to match one backwards
				seq.matchLen = uint32(lenght - zstdMinMatch)
				if repIndex > 0 && src[repIndex-1] == uint8(cv>>8) {
					addLiterals(&seq, s+1)
					seq.matchLen++
				} else {
					addLiterals(&seq, s+2)
				}
				// rep 0
				seq.offset = 1
				blk.sequences = append(blk.sequences, seq)
				s += lenght + 2
				//println("repeat sequence", seq, "next s:", s)
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
			if coffset0 < maxMatchOffset && uint32(cv) == candidate.val {
				// found a regular match
				t = candidate.offset - e.cur
				if debug && s <= t {
					panic("s <= t")
				}
				break
			}
			if coffset1 < maxMatchOffset && uint32(cv>>8) == candidate2.val {
				// found a regular match
				t = candidate2.offset - e.cur
				s++
				if debug && s <= t {
					panic("s <= t")
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

		if debug && int(offset1) > len(src)+len(e.prev) {
			panic("invalid offset")
		}

		var seq seq
		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.
		seq.litLen = uint32(s - nextEmit)

		// Call emitCopy, and then see if another emitCopy could be our next
		// move. Repeat until we find no match for the input immediately after
		// what was consumed by the last emitCopy call.
		//
		// If we exit this loop normally then we need to call emitLiteral next,
		// though we don't yet know how big the literal will be. We handle that
		// by proceeding to the next iteration of the main loop. We also can
		// exit this loop via goto if we get close to exhausting the input.

		// Invariant: we have a 4-byte match at s, and no need to emit any
		// literal bytes prior to s.

		// Extend the 4-byte match as long as possible.
		l := e.matchlen(s+4, t+4, src)

		// Extend backwards
		for t > 0 && seq.litLen > 0 && src[t-1] == src[s-1] {
			s--
			t--
			l++
			seq.litLen--
		}
		for t <= 0 && seq.litLen > 0 && s > 0 {
			off := int32(len(e.prev)) + t - 1
			if off > 0 && e.prev[off] == src[s-1] {
				s--
				t--
				l++
				seq.litLen--
				continue
			}
			break
		}
		l += 4
		seq.matchLen = uint32(l - zstdMinMatch)
		if seq.litLen > 0 {
			blk.literals = append(blk.literals, src[nextEmit:s]...)
		}
		// Don't use repeat offsets
		seq.offset = uint32(s-t) + 3
		s += l
		//println("sequence", seq, "next s:", s)
		blk.sequences = append(blk.sequences, seq)
		nextEmit = s
		if s >= sLimit {
			if debug {
				println("ended after match")
			}
			break encodeLoop
		}
		cv = load6432(src, s)
		nextHash = hash6(cv, hashLog)

		//		nextHash = hashLen(cv, hashLog, mls)
	}

	if int(nextEmit) < len(src) {
		blk.literals = append(blk.literals, src[nextEmit:]...)
		blk.extraLits = len(src) - int(nextEmit)
	}
	e.cur += int32(len(src))
	e.prev = src
	blk.recentOffsets[0] = uint32(offset1)
	blk.recentOffsets[1] = uint32(offset2)
	if debug {
		println("returning, recent offsets:", blk.recentOffsets)
	}
}

// cmp32Hist compares a value, potentially in history
// t == 0 is the start of current block.
func (e *simpleEncoder) cmp32Hist(want uint32, t int32, src []byte) bool {
	// If we are inside the current block
	if t >= 0 {
		return want == load3232(src, t)
	}
	if t > -4 {
		// We skip the boundary
		return false
	}

	tp := int32(len(e.prev)) + t
	if tp < 0 {
		return false
	}
	return load3232(e.prev, tp) == want
}

func (e *simpleEncoder) matchlen(s, t int32, src []byte) int32 {
	s1 := int(s) + maxMatchLength - 4
	if s1 > len(src) {
		s1 = len(src)
	}

	// If we are inside the current block
	if t >= 0 {
		b := src[t:]
		a := src[s:s1]
		b = b[:len(a)]
		// Extend the match to be as long as possible.
		for i := range a {
			if a[i] != b[i] {
				return int32(i)
			}
		}
		return int32(len(a))
	}

	// We found a match in the previous block.
	tp := int32(len(e.prev)) + t
	if tp < 0 {
		return 0
	}

	// Extend the match to be as long as possible.
	a := src[s:s1]
	b := e.prev[tp:]
	if len(b) > len(a) {
		b = b[:len(a)]
	}
	a = a[:len(b)]
	for i := range b {
		if a[i] != b[i] {
			return int32(i)
		}
	}

	// If we reached our limit, we matched everything we are
	// allowed to in the previous block and we return.
	n := int32(len(b))
	if int(s+n) == s1 {
		return n
	}

	// Continue looking for more matches in the current block.
	a = src[s+n : s1]
	b = src[:len(a)]
	for i := range a {
		if a[i] != b[i] {
			return int32(i) + n
		}
	}
	return int32(len(a)) + n
}

// matchLen returns a match length in src between index s and t
func matchLenIn(src []byte, s, t int32) int32 {
	s1 := len(src)
	b := src[t:]
	a := src[s:s1]
	b = b[:len(a)]
	// Extend the match to be as long as possible.
	for i := range a {
		if a[i] != b[i] {
			return int32(i)
		}
	}
	return int32(len(a))
}

// Reset the encoding table.
func (e *simpleEncoder) Reset() {
	e.prev = nil
	e.cur += maxMatchOffset
}
