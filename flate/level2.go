package flate

// fastGen maintains the table for matches,
// and the previous byte block for level 2.
// This is the generic implementation.
type fastEncL2 struct {
	fastGen
	table [bTableSize]tableEntry
}

// EncodeL2 uses a similar algorithm to level 1, but is capable
// of matching across blocks giving better compression at a small slowdown.
func (e *fastEncL2) Encode(dst *tokens, src []byte) {
	const (
		inputMargin            = 12 - 1
		minNonLiteralBlockSize = 1 + 1 + inputMargin
	)

	// Protect against e.cur wraparound.
	for e.cur >= (1<<31)-(maxStoreBlockSize*2) {
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
			if v < minOff {
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
	nextHash := hash4u(cv, bTableBits)

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
		const skipLog = 6
		const doEvery = 2

		nextS := s
		var candidate tableEntry
		for {
			s = nextS
			nextS = s + doEvery + (s-nextEmit)>>skipLog
			if nextS > sLimit {
				goto emitRemainder
			}
			candidate = e.table[nextHash&bTableMask]
			now := load6432(src, nextS)
			e.table[nextHash&bTableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hash4u(uint32(now), bTableBits)

			offset := s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				e.table[nextHash&bTableMask] = tableEntry{offset: nextS + e.cur, val: uint32(now)}
				break
			}

			// Do one right away...
			cv = uint32(now)
			s = nextS
			nextS++
			candidate = e.table[nextHash&bTableMask]
			now >>= 8
			e.table[nextHash&bTableMask] = tableEntry{offset: s + e.cur, val: cv}
			nextHash = hash4u(uint32(now), bTableBits)

			offset = s - (candidate.offset - e.cur)
			if offset < maxMatchOffset && cv == candidate.val {
				e.table[nextHash&bTableMask] = tableEntry{offset: nextS + e.cur, val: uint32(now)}
				break
			}
			cv = uint32(now)
		}

		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.

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
					e.table[hash4u(cv, bTableBits)&bTableMask] = tableEntry{offset: s + e.cur, val: cv}
				}
				goto emitRemainder
			}

			// Store every second hash in-between, but offset by 1.
			if true {
				for i := s - l + 2; i < s-5; i += 7 {
					x := load6432(src, int32(i))
					nextHash := hash4u(uint32(x), bTableBits)
					e.table[nextHash&bTableMask] = tableEntry{offset: e.cur + i, val: uint32(x)}
					// Skip one
					x >>= 16
					nextHash = hash4u(uint32(x), bTableBits)
					e.table[nextHash&bTableMask] = tableEntry{offset: e.cur + i + 2, val: uint32(x)}
					// Skip one
					x >>= 16
					nextHash = hash4u(uint32(x), bTableBits)
					e.table[nextHash&bTableMask] = tableEntry{offset: e.cur + i + 4, val: uint32(x)}
				}
			}

			// We could immediately start working at s now, but to improve
			// compression we first update the hash table at s-1 and at s. If
			// another emitCopy is not our next move, also calculate nextHash
			// at s+1. At least on GOARCH=amd64, these three hash calculations
			// are faster as one load64 call (with some shifts) instead of
			// three load32 calls.
			x := load6432(src, s-2)
			o := e.cur + s - 2
			prevHash := hash4u(uint32(x), bTableBits)
			prevHash2 := hash4u(uint32(x>>8), bTableBits)
			e.table[prevHash&bTableMask] = tableEntry{offset: o, val: uint32(x)}
			e.table[prevHash2&bTableMask] = tableEntry{offset: o + 1, val: uint32(x >> 8)}
			currHash := hash4u(uint32(x>>16), bTableBits)
			candidate = e.table[currHash&bTableMask]
			e.table[currHash&bTableMask] = tableEntry{offset: o + 2, val: uint32(x >> 16)}

			offset := s - (candidate.offset - e.cur)
			if offset > maxMatchOffset || uint32(x>>16) != candidate.val {
				cv = uint32(x >> 24)
				nextHash = hash4u(uint32(cv), bTableBits)
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
