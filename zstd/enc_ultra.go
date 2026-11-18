// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"bytes"
	"fmt"
	"math"
	"math/bits"

	"github.com/klauspost/compress/internal/le"
	"github.com/klauspost/compress/internal/regmask"
)

// The ultra encoder is a port of the reference btultra2 strategy (zstd -19):
// a binary tree match finder feeding an optimal parser with adaptive prices.

const (
	ultraOptNum     = 1 << 12 // ZSTD_OPT_NUM
	ultraSufficient = 256     // targetLength: longer matches are committed at once
	ultraMinMatch   = 3
	ultraMaxPrice   = 1 << 30
	ultraMatchBias  = 256 / 5 // favors fewer sequences
	ultraMaxSearch  = 8       // largest searchLog in ultraRows
)

// ultraRows are the reference level 19 parameters by content size.
var ultraRows = [...]struct {
	maxSize                      int64
	chainLog, hashLog, searchLog uint8
}{
	{16 << 10, 15, 15, 8},
	{128 << 10, 18, 17, 5},
	{256 << 10, 19, 19, 8},
	{math.MaxInt64, 24, 22, 7},
}

// ultraOptimal is a stretch: a match of mlen at off, followed by litlen literals.
type ultraOptimal struct {
	price  int32
	off    uint32 // 1-3: repeat code, otherwise offset+3
	mlen   int32
	litlen int32
	rep    [3]uint32 // repeat offsets after the match
}

type ultraMatch struct {
	off    uint32
	length int32
}

type ultraWork struct {
	opt     [ultraOptNum + 3]ultraOptimal
	matches [4 + 1<<ultraMaxSearch]ultraMatch
	fp      [2]ultraFingerprint
}

// ultraStats holds symbol frequencies and the derived prices, in 1/256 bits.
type ultraStats struct {
	litFreq                     [256]uint32
	llFreq                      [maxLLCode + 1]uint32
	mlFreq                      [maxMLCode + 1]uint32
	ofFreq                      [32]uint32
	litSum, llSum, mlSum, ofSum uint32

	// Per symbol parts of the prices. The sums only enter through the bases,
	// so a stored sequence updates four entries instead of every price.
	litW                       [256]int32
	llCost                     [maxLLCode + 1]int32
	mlCost                     [maxMLCode + 1]int32
	ofCost                     [32]int32
	litBase, llBase, matchBase int32
}

// ultraEncoder searches a binary tree like the reference btultra2 strategy
// and chooses sequences with its optimal parser.
// Positions are e.cur + index into e.hist; 0 is the null tree child.
type ultraEncoder struct {
	fastBase
	hashTable  []uint32
	hashTable3 []uint32
	bt         []uint32 // smaller and larger child of each node
	ws         *ultraWork
	st         ultraStats

	lowLimit     int32 // first position of the frame
	nextToUpdate int32 // first position not inserted into the tree
	btMask       int32
	nbCompares   int32
	maxLoad      int32
	hashShift    uint8
	hash3Shift   uint8

	sizeHint       int64
	firstReps      [3]uint32
	ready          bool // tables sized for this frame
	split          bool // split blocks, and find changes in the input
	firstBlock     bool
	historyPending bool

	dict *dict // dictionary of the frame
	snap ultraSnapshot
}

// ultraSnapshot holds the table entries of a loaded dictionary, so later
// frames copy them instead of inserting the dictionary again.
// It is saved on the second load, so a dictionary used once costs no memory.
// Positions are relative to the byte before the dictionary, so null entries
// restore to a position below the dictionary, which is never searched.
type ultraSnapshot struct {
	dict               *dict // last dictionary loaded with these tables, or nil
	hashLen            int
	btMask, nbCompares int32
	saved              bool // hash and bt hold the tables after loading dict
	dense              bool // hash is the whole hashTable, not pairs of index and position
	hash               []uint32
	bt                 []uint32 // children of the nodes from btStart on
	btStart            int32
}

// WindowSize records size as a hint for sizing the tables of the next frame.
func (e *ultraEncoder) WindowSize(size int64) int32 {
	e.sizeHint = max(size, 0)
	return e.fastBase.WindowSize(size)
}

// Reset will reset and set a dictionary if not nil
func (e *ultraEncoder) Reset(d *dict, singleBlock bool) {
	end := int64(e.cur) + int64(len(e.hist))
	e.resetBase(d, singleBlock)
	e.startFrame(end)
	e.dict = d
	e.firstBlock = true
	e.firstReps = [3]uint32{1, 4, 8}
	if d != nil {
		for i, o := range d.offsets {
			e.firstReps[i] = uint32(o)
		}
	}
}

// ResetPrefix starts a frame section whose history is prefix.
// The decoder's repeat offsets are unknown here.
func (e *ultraEncoder) ResetPrefix(prefix []byte) {
	end := int64(e.cur) + int64(len(e.hist))
	e.resetBasePrefix(prefix)
	e.startFrame(end)
	e.dict = nil
	e.firstBlock = false
	e.sizeHint = 0
}

// startFrame continues positions after end, so every table entry is below lowLimit.
// That avoids clearing the tables for every frame.
func (e *ultraEncoder) startFrame(end int64) {
	if end+int64(len(e.hist)) > int64(e.bufferReset) {
		clear(e.hashTable[:cap(e.hashTable)])
		clear(e.bt[:cap(e.bt)])
		clear(e.hashTable3[:cap(e.hashTable3)])
		e.snap.dict = nil
		end = 0
	}
	e.cur = int32(max(end, 2))
	e.lowLimit, e.nextToUpdate = e.cur, e.cur
	e.st.llSum = 0
	e.historyPending = len(e.hist) > 0
	e.ready = false
}

// initFrame sizes the tables from the size hint, like the reference does for a known content size.
func (e *ultraEncoder) initFrame() {
	e.ready = true
	size := e.sizeHint
	e.sizeHint = 0
	r := ultraRows[len(ultraRows)-1]
	wlog := uint8(bits.Len32(uint32(e.maxMatchOff - 1)))
	if size > 0 {
		size += int64(len(e.hist))
		for _, row := range ultraRows {
			if size <= row.maxSize {
				r = row
				break
			}
		}
		srcLog := uint8(6)
		if size >= 64 {
			srcLog = uint8(bits.Len64(uint64(size - 1)))
		}
		wlog = min(wlog, srcLog)
	}
	// The reference splits blocks from a window of 128 KiB.
	e.split = wlog >= 17
	hashLog := min(r.hashLog, wlog+1)
	chainLog := min(r.chainLog, wlog+1)
	if e.lowMem {
		hashLog = min(hashLog, 20)
		chainLog = min(chainLog, 22)
	}
	hashLog3 := min(17, max(wlog, 10))
	e.hashTable = ultraTable(e.hashTable, 1<<hashLog)
	e.bt = ultraTable(e.bt, 1<<chainLog)
	e.hashTable3 = ultraTable(e.hashTable3, 1<<hashLog3)
	e.hashShift = 32 - hashLog
	e.hash3Shift = 32 - hashLog3
	e.btMask = 1<<(chainLog-1) - 1
	e.nbCompares = 1 << r.searchLog
	e.maxLoad = 1 << max(hashLog+3, chainLog+1)
	if e.ws == nil {
		e.ws = new(ultraWork)
	}
}

func ultraTable(t []uint32, n int) []uint32 {
	if cap(t) < n {
		return make([]uint32, n)
	}
	return t[:n]
}

// rebase shifts positions down before they overflow.
// The shift is a multiple of the tree size, so nodes keep their slots.
func (e *ultraEncoder) rebase() {
	e.snap.dict = nil
	if len(e.hist) == 0 {
		clear(e.hashTable[:cap(e.hashTable)])
		clear(e.bt[:cap(e.bt)])
		clear(e.hashTable3[:cap(e.hashTable3)])
		e.cur, e.lowLimit, e.nextToUpdate = 2, 2, 2
		return
	}
	// The last position a pending history load inserts is 8 bytes before
	// its end. Keeping its window makes a dictionary load alike after a rebase.
	keep := max(e.lowLimit, e.cur+int32(len(e.hist))-e.maxMatchOff-7)
	sh := (keep - 2) &^ e.btMask
	for _, t := range [...][]uint32{e.hashTable[:cap(e.hashTable)], e.bt[:cap(e.bt)], e.hashTable3[:cap(e.hashTable3)]} {
		for i, v := range t {
			if int32(v) < keep {
				t[i] = 0
			} else {
				t[i] = v - uint32(sh)
			}
		}
	}
	// cur may go negative; positions below lowLimit are never used.
	e.cur -= sh
	e.nextToUpdate = max(e.nextToUpdate, keep) - sh
	e.lowLimit = keep - sh
}

// loadHistory inserts the dictionary or prefix into the tree.
func (e *ultraEncoder) loadHistory() {
	e.historyPending = false
	n := int32(len(e.hist))
	if n <= 8 {
		return
	}
	s := &e.snap
	d := e.dict
	same := d != nil && s.dict == d && s.hashLen == len(e.hashTable) && s.btMask == e.btMask && s.nbCompares == e.nbCompares
	if same && s.saved {
		e.restoreDict()
		return
	}
	if d != nil {
		// Only dictionary entries go into the snapshot.
		clear(e.hashTable)
	}
	// Earlier positions are outside the window of the last one inserted.
	start := max(e.lowLimit, e.cur+max(n-min(e.maxLoad, e.maxMatchOff+7), 0))
	e.nextToUpdate = start
	e.updateTree(e.cur+n-8, n)
	if d == nil {
		return
	}
	if !same {
		*s = ultraSnapshot{dict: d, hashLen: len(e.hashTable), btMask: e.btMask, nbCompares: e.nbCompares, hash: s.hash, bt: s.bt}
		return
	}

	s.saved = true
	// cur may be negative after a rebase, but inserted positions are at least start.
	base := uint32(e.cur - 1)
	rel := func(v uint32) uint32 {
		if int32(v) < start {
			return 0
		}
		return v - base
	}
	// The table holds only dictionary entries. Pairs of index and position
	// are smaller unless more than half of the slots are set.
	used := 0
	for _, v := range e.hashTable {
		if v != 0 {
			used++
		}
	}
	s.dense = 2*used > len(e.hashTable)
	if s.dense {
		s.hash = ultraTable(s.hash, len(e.hashTable))
		for i, v := range e.hashTable {
			s.hash[i] = rel(v)
		}
	} else {
		s.hash = ultraTable(s.hash, 2*used)[:0]
		for i, v := range e.hashTable {
			if v != 0 {
				s.hash = append(s.hash, uint32(i), rel(v))
			}
		}
	}
	// Each slot holds the last node that used it.
	// Slots of positions not inserted may hold entries from older frames.
	s.btStart = max(start, e.cur+n-e.btMask-1) - e.cur
	s.bt = ultraTable(s.bt, 2*int(n-s.btStart))
	for i := range int(n - s.btStart) {
		j := 2 * ((e.cur + s.btStart + int32(i)) & e.btMask)
		s.bt[2*i], s.bt[2*i+1] = rel(e.bt[j]), rel(e.bt[j+1])
	}
}

// restoreDict loads the dictionary snapshot.
// Other entries are below the dictionary, so they are never searched.
func (e *ultraEncoder) restoreDict() {
	s := &e.snap
	base := uint32(e.cur - 1)
	if s.dense {
		ultraRestore(e.hashTable, s.hash, base)
	} else {
		ht := e.hashTable
		for i := 0; i+1 < len(s.hash); i += 2 {
			ht[s.hash[i]] = s.hash[i+1] + base
		}
	}
	// The nodes take consecutive slots, wrapping around at most once.
	j := 2 * ((e.cur + s.btStart) & e.btMask)
	k := min(len(s.bt), len(e.bt)-int(j))
	ultraRestore(e.bt[j:], s.bt[:k], base)
	ultraRestore(e.bt, s.bt[k:], base)
	e.nextToUpdate = e.cur + int32(len(e.hist)) - 8
}

func ultraRestore(dst, src []uint32, base uint32) {
	dst = dst[:len(src)]
	for i, v := range src {
		dst[i] = v + base
	}
}

// Encode improves compression...
func (e *ultraEncoder) Encode(blk *blockEnc, src []byte) {
	if !e.ready {
		e.initFrame()
	}
	// The first block may be parsed twice at increasing positions.
	if int64(e.cur)+int64(len(e.hist))+2*int64(len(src)) > int64(e.bufferReset) {
		e.rebase()
	}
	if e.historyPending {
		e.loadHistory()
	}
	s := e.addBlock(src)
	blk.size = len(src)
	blk.highRatio, blk.ultra, blk.postSplit = true, true, e.split
	first := e.firstBlock
	e.firstBlock = false
	nl, ns := len(blk.literals), len(blk.sequences)
	// Other blocks start with unknown repeat offsets, since the decoder's
	// depend on how earlier blocks were finally emitted.
	var rep [3]uint32
	if first {
		rep = e.firstReps
	}
	blk.startReps = rep

	// The reference does not compress blocks this small.
	if len(src) < 7 {
		blk.literals = append(blk.literals, src...)
		blk.extraLits = len(src)
		ultraRLE(blk, src, nl, ns)
		return
	}

	start := e.cur + s
	e.nextToUpdate = max(e.nextToUpdate, e.lowLimit)
	// Limit the catch-up after a long match.
	if start > e.nextToUpdate+384 {
		e.nextToUpdate = start - min(192, start-e.nextToUpdate-384)
	}

	end := int32(len(e.hist))
	// Like the reference's block splitter, look for changes in full blocks after the first.
	split := e.split && len(src) >= maxCompressedBlockSize && e.st.llSum != 0
	if first && e.st.llSum == 0 && start == e.lowLimit && len(src) > 8 {
		// Seed the statistics with a first pass, then parse again
		// at new positions so nothing from the first pass matches.
		e.parse(blk, s, end, rep, false)
		blk.literals, blk.sequences = blk.literals[:nl], blk.sequences[:ns]
		e.cur += int32(len(src))
		e.lowLimit = e.cur + s
		e.nextToUpdate = e.lowLimit
	}
	startReps := rep
	rep, anchor := e.parse(blk, s, end, rep, split)
	if anchor < end {
		blk.literals = append(blk.literals, e.hist[anchor:]...)
		blk.extraLits = int(end - anchor)
	}
	blk.recentOffsets = rep
	ultraRLE(blk, src, nl, ns)
	if debugAsserts {
		e.verify(blk, s, startReps)
	}
}

// ultraRLE turns the output for a block of one repeated byte into a single
// match at offset 1, which blockEnc emits as an RLE block.
// The block is parsed first anyway, since the reference updates its statistics with it.
func ultraRLE(blk *blockEnc, src []byte, nl, ns int) {
	if len(src) <= zstdMinMatch || len(src) > maxCompressedBlockSize || matchLen(src[1:], src) != len(src)-1 {
		return
	}
	if len(blk.sequences) == ns+1 && blk.sequences[ns].offset == 1+3 && blk.sequences[ns].litLen <= 1 && blk.extraLits == 0 {
		return
	}
	blk.literals = append(blk.literals[:nl], src[0])
	blk.sequences = append(blk.sequences[:ns], seq{litLen: 1, matchLen: uint32(len(src)-1) - zstdMinMatch, offset: 1 + 3})
	blk.extraLits = 0
}

// ultraChunk is the step of the search for changes in the input.
const ultraChunk = 8 << 10

// ultraFingerprint counts the hashes of the 2-byte strings of some input.
type ultraFingerprint struct {
	events [1 << 10]uint32
	n      uint32
}

func (f *ultraFingerprint) record(src []byte) {
	clear(f.events[:])
	for i := range len(src) - 1 {
		f.events[uint32(le.Load16(src, i))*0x9e3779b9>>22]++
	}
	f.n = uint32(len(src) - 1)
}

// splitAt returns the start of the first chunk of src whose counts of 2-byte
// strings differ from those of the chunks before it, or len(src)
// (ZSTD_splitBlock_byChunks at the btultra2 level).
func (w *ultraWork) splitAt(src []byte) int {
	if len(src) < 2*ultraChunk {
		return len(src)
	}
	past, cur := &w.fp[0], &w.fp[1]
	past.record(src[:ultraChunk])
	penalty := uint64(3)
	for pos := ultraChunk; pos+ultraChunk <= len(src); pos += ultraChunk {
		cur.record(src[pos : pos+ultraChunk])
		var dev uint64
		for i, v := range past.events {
			a, b := uint64(v)*uint64(cur.n), uint64(cur.events[i])*uint64(past.n)
			dev += max(a, b) - min(a, b)
		}
		if dev >= uint64(past.n)*uint64(cur.n)*(14+penalty)/16 {
			return pos
		}
		for i, v := range cur.events {
			past.events[i] += v
		}
		past.n += cur.n
		penalty -= min(penalty, 1)
	}
	return len(src)
}

// EncodeNoHist will encode a block with no history and no following blocks.
func (e *ultraEncoder) EncodeNoHist(blk *blockEnc, src []byte) {
	e.ensureHist(len(src))
	e.Encode(blk, src)
}

// parse runs the optimal parser (ZSTD_compressBlock_opt_generic at optLevel 2)
// over hist[istart:iend] and appends the sequences to blk.
// It returns the repeat offsets and the start of the trailing literals.
// With split, it rescales the statistics where the input changes.
func (e *ultraEncoder) parse(blk *blockEnc, istart, iend int32, rep [3]uint32, split bool) ([3]uint32, int32) {
	src := e.hist
	st := &e.st
	opt := &e.ws.opt
	matches := &e.ws.matches
	ip, anchor := istart, istart
	ilimit := iend - 8
	nextToUpdate3 := e.nextToUpdate
	// Fields of last survive between segments, as in the reference.
	var last ultraOptimal

	st.rescale(src[istart:iend], e.dict)
	// The reference starts a block where the input changes (ZSTD_splitBlock),
	// which rescales the statistics. Here only the statistics are rescaled:
	// the extra blocks cost more than they gain after the post-block splitter.
	next := iend
	if split {
		next = istart + int32(e.ws.splitAt(src[istart:iend]))
	}
	if e.cur+ip == e.lowLimit {
		ip++
	}
	for ip < ilimit {
		if ip >= next {
			st.rescale(src[ip:iend], e.dict)
			next += int32(e.ws.splitAt(src[next:iend]))
		}
		var cur, lastPos int32
		litlen := ip - anchor
		n := e.getAllMatches(&nextToUpdate3, ip, iend, &rep, litlen == 0)
		if n == 0 {
			ip++
			continue
		}
		// Literal prices before the first match are the same for every path.
		opt[0].mlen = 0
		opt[0].litlen = litlen
		opt[0].price = st.llPrice(litlen)
		opt[0].rep = rep

		if maxML := matches[n-1].length; maxML > ultraSufficient {
			last.litlen, last.mlen, last.off = 0, maxML, matches[n-1].off
			lastPos = maxML
		} else {
			pos := int32(1)
			for ; pos < ultraMinMatch; pos++ {
				opt[pos].price = ultraMaxPrice
				opt[pos].mlen = 0
				opt[pos].litlen = litlen + pos
			}
			base := opt[0].price + st.llPrice(0) + st.matchBase
			for _, m := range matches[:n] {
				p := base + st.ofCost[ofCode(m.off)]
				for ; pos <= m.length; pos++ {
					opt[pos].mlen = pos
					opt[pos].off = m.off
					opt[pos].litlen = 0
					opt[pos].price = p + st.mlCost[mlCode(uint32(pos-zstdMinMatch))]
				}
			}
			lastPos = pos - 1
			opt[pos].price = ultraMaxPrice

			immediate := false
			for cur = 1; cur <= lastPos; cur++ {
				inr := ip + cur
				// Reach cur with one more literal, if that is cheaper.
				ll := opt[cur-1].litlen + 1
				if price := opt[cur-1].price + st.litPrice(src[inr-1]) + st.llIncPrice(ll); price <= opt[cur].price {
					prevMatch := opt[cur]
					opt[cur] = opt[cur-1]
					opt[cur].litlen = ll
					opt[cur].price = price
					if prevMatch.litlen == 0 && st.llIncPrice(1) < 0 && inr < iend {
						// The replaced match followed by one literal may still win at cur+1.
						lp := st.litPrice(src[inr])
						with1 := prevMatch.price + lp + st.llIncPrice(1)
						if with1 < price+lp+st.llIncPrice(ll+1) && with1 < opt[cur+1].price {
							prev := cur - prevMatch.mlen
							reps := ultraNewRep(opt[prev].rep, prevMatch.off, opt[prev].litlen == 0)
							opt[cur+1] = prevMatch
							opt[cur+1].rep = reps
							opt[cur+1].litlen = 1
							opt[cur+1].price = with1
							lastPos = max(lastPos, cur+1)
						}
					}
				}
				if opt[cur].litlen == 0 {
					prev := cur - opt[cur].mlen
					opt[cur].rep = ultraNewRep(opt[prev].rep, opt[cur].off, opt[prev].litlen == 0)
				}
				if inr > ilimit {
					continue
				}
				if cur == lastPos {
					break
				}
				n := e.getAllMatches(&nextToUpdate3, inr, iend, &opt[cur].rep, opt[cur].litlen == 0)
				if n == 0 {
					continue
				}
				longest := matches[n-1].length
				if longest > ultraSufficient || cur+longest >= ultraOptNum || inr+longest >= iend {
					last.mlen, last.off, last.litlen = longest, matches[n-1].off, 0
					lastPos = cur + longest
					immediate = true
					break
				}
				base := opt[cur].price + st.llPrice(0) + st.matchBase
				startML := int32(ultraMinMatch)
				for _, m := range matches[:n] {
					p := base + st.ofCost[ofCode(m.off)]
					for ml := m.length; ml >= startML; ml-- {
						pos := cur + ml
						price := p + st.mlCost[mlCode(uint32(ml-zstdMinMatch))]
						if pos > lastPos || price < opt[pos].price {
							for lastPos < pos {
								lastPos++
								opt[lastPos].price = ultraMaxPrice
								opt[lastPos].litlen = 1
							}
							opt[pos].mlen = ml
							opt[pos].off = m.off
							opt[pos].litlen = 0
							opt[pos].price = price
						}
					}
					startML = m.length + 1
				}
				opt[lastPos+1].price = ultraMaxPrice
			}
			if !immediate {
				last = opt[lastPos]
				cur = lastPos - last.mlen
			}
		}

		if last.mlen == 0 {
			// Only literals: parse on from the end.
			ip += lastPos
			continue
		}
		if last.litlen == 0 {
			rep = ultraNewRep(opt[cur].rep, last.off, opt[cur].litlen == 0)
		} else {
			// Trailing literals are parsed again with the next segment.
			rep = last.rep
			cur -= last.litlen
		}

		// Backtrack in place, turning stretches into sequences.
		// Stale entries this leaves in opt are read by later segments
		// exactly as in the reference.
		storeEnd := cur + 2
		storeStart := storeEnd
		stretchPos := cur
		opt[storeEnd] = last
		for {
			next := opt[stretchPos]
			opt[storeStart].litlen = next.litlen
			if next.mlen == 0 {
				break
			}
			storeStart--
			opt[storeStart] = next
			stretchPos -= next.litlen + next.mlen
		}
		for sp := storeStart; sp <= storeEnd; sp++ {
			llen, mlen, off := opt[sp].litlen, opt[sp].mlen, opt[sp].off
			if mlen == 0 {
				ip = anchor + llen
				continue
			}
			// Huge BuildDict samples: genCodes takes at most 64K sequences.
			if len(blk.sequences) >= math.MaxUint16 {
				return rep, anchor
			}
			lits := src[anchor : anchor+llen]
			st.update(lits, off, mlen)
			blk.literals = append(blk.literals, lits...)
			blk.sequences = append(blk.sequences, seq{litLen: uint32(llen), matchLen: uint32(mlen - zstdMinMatch), offset: off})
			anchor += llen + mlen
			ip = anchor
		}
		st.setBasePrices()
	}
	return rep, anchor
}

// getAllMatches returns the number of matches at ip found in e.ws.matches,
// in increasing length, after inserting all positions before ip into the tree.
// Repeat offsets come first, then at most one 3-byte match, then the tree.
func (e *ultraEncoder) getAllMatches(nextToUpdate3 *int32, ip, iend int32, rep *[3]uint32, ll0 bool) int {
	cur := e.cur
	curr := cur + ip
	if curr < e.nextToUpdate {
		return 0
	}
	if e.nextToUpdate < curr {
		e.updateTree(curr, iend)
	}
	src := e.hist
	matches := &e.ws.matches
	iLimit := min(iend, ip+maxMatchLength)
	windowLow := max(e.lowLimit, curr-e.maxMatchOff+1)
	bestLength := int32(ultraMinMatch - 1)
	mnum := 0

	lr := 0
	if ll0 {
		lr = 1
	}
	cv := load3232(src, ip) << 8
	for rc := lr; rc < 3+lr; rc++ {
		var repOff uint32
		if rc == 3 {
			repOff = rep[0] - 1
		} else {
			repOff = rep[rc]
		}
		// Rejects the unknown offset 0 and rep[0]-1 == 0 by wrapping.
		if repOff-1 >= uint32(curr-windowLow) {
			continue
		}
		r := ip - int32(repOff)
		if load3232(src, r)<<8 != cv {
			continue
		}
		if repLen := 3 + ultraCount(src, ip+3, r+3, iLimit); repLen > bestLength {
			bestLength = repLen
			matches[mnum] = ultraMatch{off: uint32(rc - lr + 1), length: repLen}
			mnum++
			if repLen > ultraSufficient || ip+repLen == iLimit {
				// ip is inserted later.
				return mnum
			}
		}
	}

	if bestLength < 3 {
		if i3 := e.findHash3(nextToUpdate3, curr); i3 >= windowLow && curr-i3 < 1<<18 {
			if ml := ultraCount(src, ip, i3-cur, iLimit); ml >= 3 {
				bestLength = ml
				matches[0] = ultraMatch{off: uint32(curr-i3) + 3, length: ml}
				mnum = 1
				if ml > ultraSufficient || ip+ml == iLimit {
					e.nextToUpdate = curr + 1
					return 1
				}
			}
		}
	}

	hashTable := e.hashTable
	bt := e.bt
	btMask := e.btMask
	h := ultraHash4(load3232(src, ip), e.hashShift)
	matchIndex := int32(hashTable[h])
	hashTable[h] = uint32(curr)
	btLow := max(curr-btMask, 0)
	i := 2 * (curr & btMask)
	smallerP, largerP := &bt[i], &bt[i+1]
	var dummy uint32
	var cs, cl int32
	matchEndIdx := curr + 9
	for n := e.nbCompares; n > 0 && matchIndex >= windowLow; n-- {
		next := 2 * (matchIndex & btMask)
		m := matchIndex - cur
		// Bytes before min(cs, cl) are known to match.
		ml := min(cs, cl)
		if n, ok := ultraCount8(src, ip+ml, m+ml, iLimit); ok {
			ml += n
		} else {
			ml += ultraCount(src, ip+ml, m+ml, iLimit)
		}
		if ml > bestLength {
			matchEndIdx = max(matchEndIdx, matchIndex+ml)
			bestLength = ml
			matches[mnum] = ultraMatch{off: uint32(curr-matchIndex) + 3, length: ml}
			mnum++
			if ml > ultraOptNum || ip+ml == iLimit {
				// Can't order the node; drop its children.
				break
			}
		}
		if src[m+ml] < src[ip+ml] {
			*smallerP = uint32(matchIndex)
			cs = ml
			if matchIndex <= btLow {
				smallerP = &dummy
				break
			}
			smallerP = &bt[next+1]
			matchIndex = int32(bt[next+1])
		} else {
			*largerP = uint32(matchIndex)
			cl = ml
			if matchIndex <= btLow {
				largerP = &dummy
				break
			}
			largerP = &bt[next]
			matchIndex = int32(bt[next])
		}
	}
	*smallerP, *largerP = 0, 0
	// Skip insertions inside repetitive data.
	e.nextToUpdate = matchEndIdx - 8
	return mnum
}

// updateTree inserts the positions from nextToUpdate up to target (excluded)
// into the tree, comparing no further than iend (ZSTD_updateTree).
func (e *ultraEncoder) updateTree(target, iend int32) {
	src := e.hist
	cur := e.cur
	hashTable := e.hashTable
	bt := e.bt
	btMask := e.btMask
	shift := e.hashShift
	nbCompares := e.nbCompares
	windowLow := max(e.lowLimit, target-e.maxMatchOff+1)
	var dummy uint32
	for idx := e.nextToUpdate; idx < target; {
		ip := idx - cur
		h := ultraHash4(load3232(src, ip), shift)
		matchIndex := int32(hashTable[h])
		hashTable[h] = uint32(idx)
		btLow := max(idx-btMask, 0)
		i := 2 * (idx & btMask)
		smallerP, largerP := &bt[i], &bt[i+1]
		var cs, cl int32
		bestLength := int32(8)
		matchEndIdx := idx + 9
		for n := nbCompares; n > 0 && matchIndex >= windowLow; n-- {
			next := 2 * (matchIndex & btMask)
			m := matchIndex - cur
			ml := min(cs, cl)
			if n, ok := ultraCount8(src, ip+ml, m+ml, iend); ok {
				ml += n
			} else {
				ml += ultraCount(src, ip+ml, m+ml, iend)
			}
			if ml > bestLength {
				bestLength = ml
				matchEndIdx = max(matchEndIdx, matchIndex+ml)
			}
			if ip+ml == iend {
				break
			}
			if src[m+ml] < src[ip+ml] {
				*smallerP = uint32(matchIndex)
				cs = ml
				if matchIndex <= btLow {
					smallerP = &dummy
					break
				}
				smallerP = &bt[next+1]
				matchIndex = int32(bt[next+1])
			} else {
				*largerP = uint32(matchIndex)
				cl = ml
				if matchIndex <= btLow {
					largerP = &dummy
					break
				}
				largerP = &bt[next]
				matchIndex = int32(bt[next])
			}
		}
		*smallerP, *largerP = 0, 0
		fwd := matchEndIdx - (idx + 8)
		if bestLength > 384 {
			fwd = max(fwd, min(192, bestLength-384))
		}
		idx += fwd
	}
	e.nextToUpdate = target
}

// findHash3 inserts positions up to curr into the 3-byte hash table
// and returns the latest earlier position with the hash of curr.
func (e *ultraEncoder) findHash3(nextToUpdate3 *int32, curr int32) int32 {
	src := e.hist
	cur := e.cur
	t := e.hashTable3
	shift := e.hash3Shift
	for idx := *nextToUpdate3; idx < curr; idx++ {
		t[ultraHash3(load3232(src, idx-cur), shift)] = uint32(idx)
	}
	*nextToUpdate3 = curr
	return int32(t[ultraHash3(load3232(src, curr-cur), shift)])
}

func ultraHash4(u uint32, shift uint8) uint32 {
	return (u * prime4bytes) >> (shift & regmask.Shift32ByUint8)
}

func ultraHash3(u uint32, shift uint8) uint32 {
	return ((u << 8) * prime3bytes) >> (shift & regmask.Shift32ByUint8)
}

// ultraCount returns the length of the common prefix of src[a:end] and src[b:].
// It requires b < a < end <= len(src).
func ultraCount(src []byte, a, b, end int32) int32 {
	if debugAsserts && (b < 0 || b >= a || a >= end || int(end) > len(src)) {
		panic(fmt.Sprintf("ultraCount: a %d, b %d, end %d, len %d", a, b, end, len(src)))
	}
	return int32(matchLen(src[a:end], src[b:]))
}

// ultraCount8 is the inlinable fast path of ultraCount.
// It reports false when the first 8 bytes match or end is closer.
func ultraCount8(src []byte, a, b, end int32) (int32, bool) {
	if a+8 <= end {
		if diff := le.Load64(src, a) ^ le.Load64(src, b); diff != 0 {
			return int32(bits.TrailingZeros64(diff) >> 3), true
		}
	}
	return 0, false
}

// ultraNewRep returns the repeat offsets after a sequence (ZSTD_newRep).
func ultraNewRep(r [3]uint32, offBase uint32, ll0 bool) [3]uint32 {
	if offBase > 3 {
		return [3]uint32{offBase - 3, r[0], r[1]}
	}
	rc := offBase - 1
	if ll0 {
		rc++
	}
	if rc == 0 {
		return r
	}
	c := r[0] - 1
	if rc < 3 {
		c = r[rc]
	}
	if rc >= 2 {
		r[2] = r[1]
	}
	r[1] = r[0]
	r[0] = c
	return r
}

// ultraWeight returns the fractional cost weight of a frequency, about 256*(1+log2(stat+1)).
func ultraWeight(stat uint32) int32 {
	s := uint64(stat) + 1
	hb := uint(bits.Len64(s) - 1)
	return int32(hb<<8) + int32((s<<8)>>(hb&regmask.Shift64ByUint))
}

func (st *ultraStats) litPrice(b byte) int32 {
	// Every literal costs at least one bit.
	return st.litBase - min(st.litW[b], st.litBase-256)
}

func (st *ultraStats) llPrice(l int32) int32 {
	if l >= maxCompressedBlockSize {
		return 256 + st.llBase + st.llCost[maxLLCode]
	}
	return st.llBase + st.llCost[llCode(uint32(l))]
}

func (st *ultraStats) llIncPrice(l int32) int32 {
	return st.llPrice(l) - st.llPrice(l-1)
}

var (
	ultraInitLL = [maxLLCode + 1]uint32{4, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	ultraInitOF = [32]uint32{6, 2, 1, 1, 2, 3, 4, 4, 4, 3, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
)

// rescale initializes the statistics on the first parse of a frame and
// scales them down on later ones (ZSTD_rescaleFreqs).
func (st *ultraStats) rescale(src []byte, d *dict) {
	if st.llSum != 0 {
		st.litSum = ultraScale(st.litFreq[:], 12)
		st.llSum = ultraScale(st.llFreq[:], 11)
		st.mlSum = ultraScale(st.mlFreq[:], 11)
		st.ofSum = ultraScale(st.ofFreq[:], 11)
	} else if !st.seed(d) {
		clear(st.litFreq[:])
		for _, b := range src {
			st.litFreq[b]++
		}
		st.litSum = 0
		for i, v := range st.litFreq {
			if v > 0 {
				v = 1 + v>>8
			}
			st.litFreq[i] = v
			st.litSum += v
		}
		st.llFreq = ultraInitLL
		st.llSum = ultraSum(st.llFreq[:])
		for i := range st.mlFreq {
			st.mlFreq[i] = 1
		}
		st.mlSum = maxMLCode + 1
		st.ofFreq = ultraInitOF
		st.ofSum = ultraSum(st.ofFreq[:])
	}
	for i, f := range st.litFreq {
		st.litW[i] = ultraWeight(f)
	}
	for i, f := range st.llFreq {
		st.llCost[i] = int32(llBitsTable[i])<<8 - ultraWeight(f)
	}
	for i, f := range st.mlFreq {
		st.mlCost[i] = int32(mlBitsTable[i])<<8 - ultraWeight(f) + ultraMatchBias
	}
	for i, f := range st.ofFreq {
		st.ofCost[i] = int32(i)<<8 - ultraWeight(f)
	}
	st.setBasePrices()
}

// seed initializes the statistics from the code lengths of the dictionary tables.
// Like the reference, it requires a literal table that codes every byte.
func (st *ultraStats) seed(d *dict) bool {
	if d == nil || d.litEnc == nil || d.llEnc == nil || d.mlEnc == nil || d.ofEnc == nil {
		return false
	}
	var lit [256]uint8
	if !d.litEnc.TableBits(&lit) || bytes.IndexByte(lit[:], 0) >= 0 {
		return false
	}
	st.litSum = 0
	for i, b := range lit {
		st.litFreq[i] = 1 << (11 - b)
		st.litSum += st.litFreq[i]
	}
	st.llSum = ultraSeedFSE(st.llFreq[:], d.llEnc)
	st.mlSum = ultraSeedFSE(st.mlFreq[:], d.mlEnc)
	st.ofSum = ultraSeedFSE(st.ofFreq[:], d.ofEnc)
	return true
}

// ultraSeedFSE fills f from the longest code of each symbol in enc (FSE_getMaxNbBits)
// and returns the sum.
func ultraSeedFSE(f []uint32, enc *fseEncoder) (sum uint32) {
	for i := range f {
		// The reference codes absent symbols in tableLog+1 bits.
		nb := uint32(enc.actualTableLog) + 1
		if i < int(enc.symbolLen) && enc.norm[i] != 0 {
			nb = (enc.ct.symbolTT[i].deltaNbBits + 0xFFFF) >> 16
		}
		f[i] = 1 << (10 - nb)
		sum += f[i]
	}
	return sum
}

func (st *ultraStats) setBasePrices() {
	st.litBase = ultraWeight(st.litSum)
	st.llBase = ultraWeight(st.llSum)
	st.matchBase = ultraWeight(st.mlSum) + ultraWeight(st.ofSum)
}

// update adds a stored sequence to the statistics (ZSTD_updateStats).
func (st *ultraStats) update(lits []byte, offBase uint32, ml int32) {
	for _, b := range lits {
		st.litFreq[b] += 2
		st.litW[b] = ultraWeight(st.litFreq[b])
	}
	st.litSum += 2 * uint32(len(lits))
	lc := min(llCode(uint32(len(lits))), maxLLCode)
	st.llFreq[lc]++
	st.llSum++
	st.llCost[lc] = int32(llBitsTable[lc])<<8 - ultraWeight(st.llFreq[lc])
	oc := ofCode(offBase)
	st.ofFreq[oc]++
	st.ofSum++
	st.ofCost[oc] = int32(oc)<<8 - ultraWeight(st.ofFreq[oc])
	mc := mlCode(uint32(ml - zstdMinMatch))
	st.mlFreq[mc]++
	st.mlSum++
	st.mlCost[mc] = int32(mlBitsTable[mc])<<8 - ultraWeight(st.mlFreq[mc]) + ultraMatchBias
}

func ultraSum(t []uint32) (sum uint32) {
	for _, v := range t {
		sum += v
	}
	return sum
}

// ultraScale scales t down to a sum of about 1<<logTarget (ZSTD_scaleStats).
func ultraScale(t []uint32, logTarget uint) uint32 {
	sum := ultraSum(t)
	factor := sum >> logTarget
	if factor <= 1 {
		return sum
	}
	shift := uint(bits.Len32(factor) - 1)
	sum = 0
	for i, v := range t {
		v = 1 + v>>shift
		t[i] = v
		sum += v
	}
	return sum
}

// verify replays the block's sequences against the history.
func (e *ultraEncoder) verify(blk *blockEnc, s int32, rep [3]uint32) {
	pos, lits := s, 0
	lowest := max(e.lowLimit-e.cur, 0)
	for i, sq := range blk.sequences {
		pos += int32(sq.litLen)
		if !bytes.Equal(blk.literals[lits:lits+int(sq.litLen)], e.hist[pos-int32(sq.litLen):pos]) {
			panic(fmt.Sprintf("seq %d: literal mismatch", i))
		}
		lits += int(sq.litLen)
		var off uint32
		if sq.offset > 3 {
			off = sq.offset - 3
		} else {
			rc := sq.offset - 1
			if sq.litLen == 0 {
				rc++
			}
			if rc == 3 {
				off = rep[0] - 1
			} else {
				off = rep[rc]
			}
		}
		ml := int32(sq.matchLen) + zstdMinMatch
		if off == 0 || int32(off) >= e.maxMatchOff || pos-int32(off) < lowest || int(pos+ml) > len(e.hist) {
			panic(fmt.Sprintf("seq %d: invalid offset %d (code %d, reps %v) at %d, len %d, lowest %d", i, off, sq.offset, rep, pos, ml, lowest))
		}
		if !bytes.Equal(e.hist[pos:pos+ml], e.hist[pos-int32(off):pos-int32(off)+ml]) {
			panic(fmt.Sprintf("seq %d: match mismatch at %d, offset %d, len %d", i, pos, off, ml))
		}
		rep = ultraNewRep(rep, sq.offset, sq.litLen == 0)
		pos += ml
	}
	if len(blk.literals)-lits != len(e.hist)-int(pos) {
		panic(fmt.Sprintf("trailing literals: %d != %d", len(blk.literals)-lits, len(e.hist)-int(pos)))
	}
}
