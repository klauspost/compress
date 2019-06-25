package flate

// fastGen maintains the table for matches,
// and the previous byte block for level 2.
// This is the generic implementation.
type fastEncL1 struct {
	fastGen
	table [tableSize]tableEntry
}

// EncodeL1 uses a similar algorithm to level 1
func (e *fastEncL1) Encode(dst *tokens, src []byte) {
	const (
		inputMargin            = 12 - 1
		minNonLiteralBlockSize = 1 + 1 + inputMargin
	)

	// Protect against e.cur wraparound.
	for e.cur >= bufferReset {
		if len(e.hist) == 0 {
			for i := range e.table[:] {
				e.table[i] = tableEntry{}
			}
			e.cur = maxMatchOffset
			break
		}
		// Shift down everything in the table that isn't already too far away.
		minOff := e.cur + int32(len(e.hist)) - maxMatchOffset
		for i := range e.table[:] {
			v := e.table[i].offset
			if v <= minOff {
				v = 0
			} else {
				v = v - e.cur + maxMatchOffset
			}
			e.table[i].offset = v
		}
		e.cur = maxMatchOffset
	}

	s := e.addBlock(src)

	// This check isn't in the Snappy implementation, but there, the caller
	// instead of the callee handles this case.
	if len(src) < minNonLiteralBlockSize {
		// We do not fill the token table.
		// This will be picked up by caller.
		dst.n = uint16(len(src))
		return
	}

	// Override src
	src = e.hist
	nextEmit := s

	// sLimit is when to stop looking for offset/length copies. The inputMargin
	// lets us use a fast path for emitLiteral in the main loop, while we are
	// looking for copies.
	sLimit := int32(len(src) - inputMargin)

	// nextEmit is where in src the next emitLiteral should start from.
	cv := load3232(src, s)
	nextHash := hash(cv)

	for {
		const skipLog = 5
		const doEvery = 2

		nextS := s
		var candidate tableEntry
		for {
			s = nextS
			nextS = s + doEvery + (s-nextEmit)>>skipLog
			if nextS > sLimit {
				goto emitRemainder
			}
			candidate = e.table[nextHash&tableMask]
			now := load6432(src, nextS)
			e.table[nextHash&tableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hash(uint32(now))

			offset := s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				e.table[nextHash&tableMask] = tableEntry{offset: nextS + e.cur, val: uint32(now)}
				break
			}

			// Do one right away...
			cv = uint32(now)
			s = nextS
			nextS++
			candidate = e.table[nextHash&tableMask]
			now >>= 8
			e.table[nextHash&tableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hash(uint32(now))

			offset = s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				e.table[nextHash&tableMask] = tableEntry{offset: nextS + e.cur, val: uint32(now)}
				break
			}
			cv = uint32(now)
		}

		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.
		for {
			// Invariant: we have a 4-byte match at s, and no need to emit any
			// literal bytes prior to s.

			// Extend the 4-byte match as long as possible.
			t := candidate.offset - e.cur
			l := e.matchlen(s+4, t+4, src) + 4

			// Extend backwards
			tMin := s - maxMatchOffset
			if tMin < 0 {
				tMin = 0
			}
			for t > tMin && s > nextEmit && src[t-1] == src[s-1] && l < maxMatchLength {
				s--
				t--
				l++
			}
			if nextEmit < s {
				emitLiteral(dst, src[nextEmit:s])
			}

			// matchToken is flate's equivalent of Snappy's emitCopy. (length,offset)
			dst.AddMatch(uint32(l-baseMatchLength), uint32(s-t-baseMatchOffset))
			s += l
			nextEmit = s
			if nextS >= s {
				s = nextS + 1
			}
			if s >= sLimit {
				// Index first pair after match end.
				if int(s+l+4) < len(src) {
					cv := load3232(src, s)
					e.table[hash(cv)&tableMask] = tableEntry{offset: s + e.cur, val: cv}
				}
				goto emitRemainder
			}

			// We could immediately start working at s now, but to improve
			// compression we first update the hash table at s-2 s-1 and at s. If
			// another emitCopy is not our next move, also calculate nextHash
			// at s+1. At least on GOARCH=amd64, these three hash calculations
			// are faster as one load64 call (with some shifts) instead of
			// three load32 calls.
			x := load6432(src, s-2)
			o := e.cur + s - 2
			prevHash := hash(uint32(x))
			prevHash2 := hash(uint32(x >> 8))
			e.table[prevHash&tableMask] = tableEntry{offset: o, val: uint32(x)}
			e.table[prevHash2&tableMask] = tableEntry{offset: o + 1, val: uint32(x >> 8)}
			currHash := hash(uint32(x >> 16))
			candidate = e.table[currHash&tableMask]
			e.table[currHash&tableMask] = tableEntry{offset: o + 2, val: uint32(x >> 16)}

			offset := s - (candidate.offset - e.cur)
			if offset > maxMatchOffset || uint32(x>>16) != candidate.val {
				cv = uint32(x >> 24)
				nextHash = hash(cv)
				s++
				break
			}
		}
	}

emitRemainder:
	if int(nextEmit) < len(src) {
		emitLiteral(dst, src[nextEmit:])
	}
}
