package flate

import "fmt"

type fastEncL6 struct {
	fastGen
	table  [tableSize]tableEntry
	bTable [tableSize]tableEntryPrev
}

func (e *fastEncL6) Encode(dst *tokens, src []byte) {
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
				e.bTable[i] = tableEntryPrev{}
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
			v := e.bTable[i]
			if v.Cur.offset <= minOff {
				v.Cur.offset = 0
				v.Prev.offset = 0
			} else {
				v.Cur.offset = v.Cur.offset - e.cur + maxMatchOffset
				if v.Prev.offset <= minOff {
					v.Prev.offset = 0
				} else {
					v.Prev.offset = v.Prev.offset - e.cur + maxMatchOffset
				}
			}
			e.bTable[i] = v
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
	nextHashS := hash4x64(cv, tableBits)
	nextHashL := hash7(cv, tableBits)
	// Repeat MUST be > 1 and within rabge
	repeat := int32(1)

	for {
		const skipLog = 6
		const doEvery = 1

		nextS := s
		var l int32
		var t int32
		for {
			s = nextS
			nextS = s + doEvery + (s-nextEmit)>>skipLog
			if nextS > sLimit {
				goto emitRemainder
			}
			// Fetch a short+long candidate
			sCandidate := e.table[nextHashS&tableMask]
			lCandidate := e.bTable[nextHashL&tableMask]
			next := load6432(src, nextS)
			entry := tableEntry{offset: s + e.cur, val: uint32(cv)}
			e.table[nextHashS&tableMask] = entry
			eLong := &e.bTable[nextHashL&tableMask]
			eLong.Cur, eLong.Prev = entry, eLong.Cur

			// Calculate hashes of 'next'
			nextHashS = hash4x64(next, tableBits)
			nextHashL = hash7(next, tableBits)

			t = lCandidate.Cur.offset - e.cur
			if s-t < maxMatchOffset {
				if uint32(cv) == lCandidate.Cur.val {
					// Store the next match
					e.table[nextHashS&tableMask] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
					eLong := &e.bTable[nextHashL&tableMask]
					eLong.Cur, eLong.Prev = tableEntry{offset: nextS + e.cur, val: uint32(next)}, eLong.Cur

					t2 := lCandidate.Prev.offset - e.cur
					if s-t2 < maxMatchOffset && uint32(cv) == lCandidate.Prev.val {
						l = e.matchlen(s+4, t+4, src) + 4
						ml1 := e.matchlen(s+4, t2+4, src) + 4
						if ml1 > l {
							t = t2
							l = ml1
							break
						}
					}
					break
				}
				t = lCandidate.Prev.offset - e.cur
				if s-t < maxMatchOffset && uint32(cv) == lCandidate.Prev.val {
					// Store the next match
					e.table[nextHashS&tableMask] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
					eLong := &e.bTable[nextHashL&tableMask]
					eLong.Cur, eLong.Prev = tableEntry{offset: nextS + e.cur, val: uint32(next)}, eLong.Cur
					break
				}
			}

			t = sCandidate.offset - e.cur
			if s-t < maxMatchOffset && uint32(cv) == sCandidate.val {
				// Found a 4 match...
				l = e.matchlen(s+4, t+4, src) + 4
				lCandidate = e.bTable[nextHashL&tableMask]

				// Store the next match
				e.table[nextHashS&tableMask] = tableEntry{offset: nextS + e.cur, val: uint32(next)}
				eLong := &e.bTable[nextHashL&tableMask]
				eLong.Cur, eLong.Prev = tableEntry{offset: nextS + e.cur, val: uint32(next)}, eLong.Cur

				// Check repeat at s
				const repOff = 1
				t2 := s - repeat + repOff
				if load3232(src, t2) == uint32(cv>>(9*repOff)) {
					ml := e.matchlen(s+4+repOff, t2+4, src) + 4
					if ml > l {
						t = t2
						l = ml
						s += repOff
						// Not worth checking more.
						break
					}
				}

				// If the next long is a candidate, use that...
				t2 = lCandidate.Cur.offset - e.cur
				if nextS-t2 < maxMatchOffset {
					if lCandidate.Cur.val == uint32(next) {
						ml := e.matchlen(nextS+4, t2+4, src) + 4
						if ml > l {
							t = t2
							s = nextS
							l = ml
						}
					}
					// If the previous long is a candidate, use that...
					t2 = lCandidate.Prev.offset - e.cur
					if nextS-t2 < maxMatchOffset && lCandidate.Prev.val == uint32(next) {
						ml := e.matchlen(nextS+4, t2+4, src) + 4
						if ml > l {
							t = t2
							s = nextS
							l = ml
							break
						}
					}
				}
				break
			}
			cv = next
		}

		// A 4-byte match has been found. We'll later see if more than 4 bytes
		// match. But, prior to the match, src[nextEmit:s] are unmatched. Emit
		// them as literal bytes.

		// Extend the 4-byte match as long as possible.
		if l == 0 {
			l = e.matchlen(s+4, t+4, src) + 4
		}

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
				panic(fmt.Sprintln("s-t", s, t))
			}
			if l > maxMatchLength {
				panic("mml")
			}
			if (s - t) > maxMatchOffset {
				panic(fmt.Sprintln("mmo", s-t))
			}
			if l < baseMatchLength {
				panic("bml")
			}
		}

		dst.AddMatch(uint32(l-baseMatchLength), uint32(s-t-baseMatchOffset))
		repeat = s - t
		s += l
		nextEmit = s
		if nextS >= s {
			s = nextS + 1
		}
		if nextS >= s {
			s = nextS + 1
		}

		if s >= sLimit {
			// Index first pair after match end.
			for i := nextS + 1; i < int32(len(src))-8; i += 2 {
				cv := load6432(src, i)
				e.table[hash4x64(cv, tableBits)&tableMask] = tableEntry{offset: i + e.cur, val: uint32(cv)}
				eLong := &e.bTable[hash7(cv, tableBits)&tableMask]
				eLong.Cur, eLong.Prev = tableEntry{offset: i + e.cur, val: uint32(cv)}, eLong.Cur
			}
			goto emitRemainder
		}

		// Store every hash in-between
		if true {
			for i := nextS + 1; i < s; i++ {
				cv := load6432(src, i)
				t := tableEntry{offset: i + e.cur, val: uint32(cv)}
				e.table[hash4x64(cv, tableBits)&tableMask] = t
				eLong := &e.bTable[hash7(cv, tableBits)&tableMask]
				eLong.Cur, eLong.Prev = t, eLong.Cur
			}
		}

		// We could immediately start working at s now, but to improve
		// compression we first update the hash table at s-1 and at s.
		x := load6432(src, s)
		nextHashS = hash4x64(x, tableBits)
		nextHashL = hash7(x, tableBits)
		cv = x
	}

emitRemainder:
	if int(nextEmit) < len(src) {
		emitLiteral(dst, src[nextEmit:])
	}
}
