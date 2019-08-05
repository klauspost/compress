package flate

import "fmt"

func (e *fastEncL4) Encode(dst *tokens, src []byte) {
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
			for i := range e.bTable[:] {
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
		for i := range e.bTable[:] {
			v := e.bTable[i].offset
			if v <= minOff {
				v = 0
			} else {
				v = v - e.cur + maxMatchOffset
			}
			e.bTable[i].offset = v
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
	cv := load6432(src, s)
	for {
		const skipLog = 6
		const doEvery = 1

		nextS := s
		var t int32
		for {
			nextHashS := hash4x64(cv, tableBits)
			nextHashL := hash7(cv, tableBits)

			s = nextS
			nextS = s + doEvery + (s-nextEmit)>>skipLog
			if nextS > sLimit {
				goto emitRemainder
			}
			// Fetch a short+long candidate
			sCandidate := e.table[nextHashS]
			lCandidate := e.bTable[nextHashL]
			next := load6432(src, nextS)
			entry := tableEntry{offset: s + e.cur, val: uint32(cv)}
			e.table[nextHashS] = entry
			e.bTable[nextHashL] = entry

			nextHashS = hash4x64(next, tableBits)
			nextHashL = hash7(next, tableBits)

			t = lCandidate.offset - e.cur
			if s-t < maxMatchOffset && uint32(cv) == lCandidate.val {
				// Store the next match
				e.table[nextHashS] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
				e.bTable[nextHashL] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
				break
			}

			t = sCandidate.offset - e.cur
			if s-t < maxMatchOffset && uint32(cv) == sCandidate.val {
				// Found a 4 match...
				lCandidate = e.bTable[nextHashL]
				// Store the next match
				e.table[nextHashS] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
				e.bTable[nextHashL] = tableEntry{offset: nextS + e.cur, val: uint32(next)}

				// If the next long is a candidate, use that...
				if nextS-(lCandidate.offset-e.cur) < maxMatchOffset && lCandidate.val == uint32(next) {
					s = nextS
					t = lCandidate.offset - e.cur
				}
				break
			}
			cv = next
		}

		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.

		// Extend the 4-byte match as long as possible.
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
		if false {
			if t >= s {
				panic("s-t")
			}
			if l > maxMatchLength {
				panic("mml")
			}
			if (s - t) > maxMatchOffset {
				panic(fmt.Sprintln("mmo", t))
			}
			if l < baseMatchLength {
				panic("bml")
			}
		}

		dst.AddMatch(uint32(l-baseMatchLength), uint32(s-t-baseMatchOffset))
		s += l
		nextEmit = s
		if nextS >= s {
			s = nextS + 1
		}

		if s >= sLimit {
			// Index first pair after match end.
			if int(s+8) < len(src) {
				cv := load6432(src, s)
				e.table[hash4x64(cv, tableBits)] = tableEntry{offset: s + e.cur, val: uint32(cv)}
				e.bTable[hash7(cv, tableBits)] = tableEntry{offset: s + e.cur, val: uint32(cv)}
			}
			goto emitRemainder
		}

		// Store every 4th hash in-between
		if true {
			for i := s - l + 4; i < s-2; i += 4 {
				cv := load6432(src, i)
				t := tableEntry{offset: i + e.cur, val: uint32(cv)}
				e.table[hash4x64(cv, tableBits)] = t
				e.bTable[hash7(cv, tableBits)] = t
			}
		}

		// We could immediately start working at s now, but to improve
		// compression we first update the hash table at s-1 and at s.
		x := load6432(src, s-1)
		o := e.cur + s - 1
		prevHashS := hash4x64(x, tableBits)
		prevHashL := hash7(x, tableBits)
		e.table[prevHashS] = tableEntry{offset: o, val: uint32(x)}
		e.bTable[prevHashL] = tableEntry{offset: o, val: uint32(x)}
		cv = x >> 8
	}

emitRemainder:
	if int(nextEmit) < len(src) {
		emitLiteral(dst, src[nextEmit:])
	}
}
