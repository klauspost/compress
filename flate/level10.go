// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package flate

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/klauspost/compress/internal/le"
)

// UltraCompression (level 10) finds all matches with a binary tree and
// chooses among them with an optimal parser, iterated on the Huffman codes
// of each block.

const (
	ultraChunk       = 512 << 10 // input parsed at a time
	ultraHashLog     = 16
	ultraHash3Log    = 16
	ultraBtLog       = 15
	ultraBtMask      = 1<<ultraBtLog - 1
	ultraMaxDist     = maxMatchOffset - 1 // a node 32768 back shares the slot of the current one
	ultraDepth       = 300                // tree nodes visited per position
	ultraNice        = maxMatchLength     // matches this long are taken at once
	ultraPasses      = 10
	ultraGainShift   = 12  // stop iterating when a pass saves less than this fraction
	ultraFewShift    = 6   // regions with less than this fraction matched by the greedy parse get one pass
	ultraSmall       = 256 // smaller regions get one pass
	ultraGreedyMin   = 4   // shortest match taken by the parse that sets the first costs
	ultraRegionSpan  = 8   // lengths below the longest of a match tried by region passes
	ultraSplitPasses = 2   // passes over a region before splitting it into blocks
	ultraSplitUnit   = 512 // tokens between possible block starts
	ultraSplitTries  = 64  // block starts tried at first when splitting a range

	// Estimated header size of a dynamic block.
	ultraHeaderBase   = 70
	ultraHeaderPerSym = 4
	ultraBlockTokens  = maxStoreBlockSize - 1 // tokens.n must also hold the EOB
	ultraBit          = 16                    // cost units per bit
	ultraCacheLimit   = ultraChunk * 6        // cache entries before a region is cut short

	ultraLitNoStat = 13 // bits for symbols the last parse did not use
	ultraLenNoStat = 13
	ultraOffNoStat = 10
)

type ultraCosts struct {
	lit    [256]uint32
	length [maxMatchLength + 1]uint32 // by match length
	off    [32]uint32                 // by offset code, including extra bits
}

type ultraFreqs struct {
	lit [literalCount]uint32 // literals, EOB and length codes
	off [32]uint32
}

// ultraEnc holds the state of level 10.
// Positions are e.cur + index into e.hist; 0 is the null tree child.
type ultraEnc struct {
	hist         []byte // history followed by input not parsed yet
	cur          int32
	parsed       int32 // index in hist of the first byte not parsed
	lowLimit     int32 // first position of the stream (or dictionary)
	nextToUpdate int32 // first position not inserted into the tree
	next3        int32 // first position not inserted into hashTable3
	bufferReset  int32

	// cache holds per position its matches (see ultraEntry),
	// in increasing length, followed by their number at index hdr[position].
	cache      []uint32
	hdr        []int32
	src        []byte   // the region being parsed
	base       int32    // index of src in hist
	cost       []uint32 // per position: cost to the end of the block
	choice     []uint32 // per position: an entry like the cache's, or 0 for a literal
	bestChoice []uint32 // choices of the best pass over a block
	matches    [ultraDepth + 2]uint32

	// Block splitting: prefix counts of symbols at the start of each unit.
	pre     []uint32
	unitPos []int32
	ends    []int32

	costs ultraCosts
	freqs ultraFreqs
	sw    *huffmanBitWriter // scratch for building codes
	tok   tokens

	// Indexes into these are masked or shifted to their size,
	// so they need no bounds checks.
	hashTable  [1 << ultraHashLog]uint32
	hashTable3 [1 << ultraHash3Log]uint32
	bt         [1 << ultraBtLog][2]uint32 // smaller and larger child of each node
}

// newUltraEnc is called on the first write or dictionary,
// so a Writer that is never used does not hold the tables.
func newUltraEnc() *ultraEnc {
	return &ultraEnc{
		cur: 2, lowLimit: 2, nextToUpdate: 2, next3: 2,
		bufferReset: math.MaxInt32 - 2*(ultraChunk+maxMatchOffset+maxMatchLength),
		sw:          newHuffmanBitWriter(nil),
	}
}

// reset starts a new stream. Positions continue after the old ones,
// so every table entry is below lowLimit and never used.
func (e *ultraEnc) reset() {
	end := int64(e.cur) + int64(len(e.hist))
	if end+maxMatchOffset+ultraChunk >= int64(e.bufferReset) {
		clear(e.hashTable[:])
		clear(e.hashTable3[:])
		clear(e.bt[:])
		end = 2
	}
	e.cur = int32(end)
	e.hist = e.hist[:0]
	e.parsed = 0
	e.lowLimit, e.nextToUpdate, e.next3 = e.cur, e.cur, e.cur
}

// loadDict makes the last 32 KiB of dict the history.
func (e *ultraEnc) loadDict(dict []byte) {
	if len(dict) > maxMatchOffset {
		dict = dict[len(dict)-maxMatchOffset:]
	}
	e.hist = append(e.hist[:0], dict...)
	n := int32(len(dict))
	e.parsed = n
	if n <= 8 {
		return
	}
	e.updateTree(e.cur+n-8, n)
}

// pending returns the index of the first position not inserted into all tables.
// Positions after a long match, or too close to the end to search, are
// inserted later, so their window must stay.
func (e *ultraEnc) pending() int32 {
	return min(e.parsed, e.nextToUpdate-e.cur, e.next3-e.cur)
}

// slide drops history that no match can reach.
func (e *ultraEnc) slide() {
	if drop := e.pending() - maxMatchOffset; drop > 0 {
		n := copy(e.hist, e.hist[drop:])
		e.hist = e.hist[:n]
		e.cur += drop
		e.parsed -= drop
	}
}

// rebase shifts positions down before they overflow.
// The shift is a multiple of the tree size, so nodes keep their slots.
func (e *ultraEnc) rebase() {
	keep := max(e.lowLimit, e.cur+e.pending()-ultraMaxDist-1)
	sh := (keep - 2) &^ ultraBtMask
	ultraShift(e.hashTable[:], keep, sh)
	ultraShift(e.hashTable3[:], keep, sh)
	for i := range e.bt {
		ultraShift(e.bt[i][:], keep, sh)
	}
	// cur may go negative; positions below lowLimit are never used.
	e.cur -= sh
	e.nextToUpdate = max(e.nextToUpdate, keep) - sh
	e.next3 = max(e.next3, keep) - sh
	e.lowLimit = keep - sh
}

// ultraShift moves the positions in t down by sh, and clears those below keep.
func ultraShift(t []uint32, keep, sh int32) {
	for i, v := range t {
		if int32(v) < keep {
			t[i] = 0
		} else {
			t[i] = v - uint32(sh)
		}
	}
}

// encode adds src to the input and writes blocks for all of it,
// except the last bytes when not flushing, since matches may continue there.
func (e *ultraEnc) encode(w *huffmanBitWriter, src []byte, sync bool) {
	e.slide()
	if int64(e.cur)+int64(len(e.hist))+int64(len(src)) >= int64(e.bufferReset) {
		e.rebase()
	}
	e.hist = append(e.hist, src...)
	end := int32(len(e.hist))
	to := end
	if !sync {
		to -= maxMatchLength
	}
	for e.parsed < to && w.err == nil {
		start := e.parsed
		e.nextToUpdate = max(e.nextToUpdate, e.lowLimit)
		e.next3 = max(e.next3, e.lowLimit)
		rend := e.findMatches(start, to, end)
		e.parseRegion(w, start, rend)
		e.parsed = rend
	}
}

// findMatches caches the matches of positions from start, up to to or until
// the cache is full, and returns the index after the last position cached.
// Matches are compared up to iend.
func (e *ultraEnc) findMatches(start, to, iend int32) int32 {
	cache := e.cache[:0]
	hdr := e.hdr[:0]
	ip := start
	for ip < to && len(cache) < ultraCacheLimit {
		n := 0
		if ip+4 <= iend {
			n = e.getMatches(ip, iend)
		}
		cache = append(cache, e.matches[:n]...)
		hdr = append(hdr, int32(len(cache)))
		cache = append(cache, uint32(n))
		ip++
		if n > 0 {
			if l := int32(e.matches[n-1] >> 16 & 0x1ff); l >= ultraNice {
				// Skipped positions are inserted into the tree by the next search.
				for range l - 1 {
					hdr = append(hdr, int32(len(cache)))
					cache = append(cache, 0)
				}
				ip += l - 1
			}
		}
	}
	e.cache, e.hdr = cache, hdr
	return ip
}

// getMatches stores the matches at index ip in e.matches, in increasing
// length, and returns their number. Earlier positions are inserted into the
// tree first.
func (e *ultraEnc) getMatches(ip, iend int32) int {
	curr := e.cur + ip
	if e.nextToUpdate < curr {
		e.updateTree(curr, iend)
	}
	e.nextToUpdate = curr + 1
	bestLength := int32(2)
	mnum := 0
	if i3 := e.findHash3(curr); i3 >= max(e.lowLimit, curr-ultraMaxDist) &&
		(le.Load32(e.hist, ip)^le.Load32(e.hist, i3-e.cur))&0xffffff == 0 {
		if ml := ultraCount(e.hist, ip, i3-e.cur, min(iend, ip+maxMatchLength)); ml >= 3 {
			bestLength = ml
			e.matches[0] = ultraEntry(ml, uint32(curr-i3))
			mnum = 1
		}
	}
	return e.btInsert(ip, iend, true, mnum, bestLength)
}

// updateTree inserts the positions from nextToUpdate up to target (excluded)
// into the tree, comparing no further than iend.
func (e *ultraEnc) updateTree(target, iend int32) {
	for idx := e.nextToUpdate; idx < target; idx++ {
		e.btInsert(idx-e.cur, iend, false, 0, 0)
	}
	e.nextToUpdate = target
}

// btInsert makes the position at index ip the root of its tree, comparing
// up to the maximum match length and no further than iend.
// With record, the matches longer than bestLength are appended to
// e.matches from mnum on, and their new number is returned.
func (e *ultraEnc) btInsert(ip, iend int32, record bool, mnum int, bestLength int32) int {
	cur := e.cur
	curr := cur + ip
	src := e.hist
	bt := &e.bt
	limit := min(iend, ip+maxMatchLength)
	h := ultraHash4(le.Load32(src, ip))
	matchIndex := int32(e.hashTable[h])
	e.hashTable[h] = uint32(curr)
	windowLow := max(e.lowLimit, curr-ultraMaxDist)
	node := &bt[uint32(curr)&ultraBtMask]
	smallerP, largerP := &node[0], &node[1]
	var cs, cl int32
	for n := ultraDepth; n > 0 && matchIndex >= windowLow; n-- {
		next := &bt[uint32(matchIndex)&ultraBtMask]
		m := matchIndex - cur
		// Bytes before min(cs, cl) are known to match.
		ml := min(cs, cl)
		if n, ok := ultraCount8(src, ip+ml, m+ml, limit); ok {
			ml += n
		} else {
			ml += ultraCount(src, ip+ml, m+ml, limit)
		}
		if ml > bestLength {
			bestLength = ml
			if record {
				e.matches[mnum] = ultraEntry(ml, uint32(curr-matchIndex))
				mnum++
			}
		}
		if ip+ml == limit {
			if ml == maxMatchLength {
				// The node equals the new one as far as any search compares,
				// so the new one takes its place and children.
				*smallerP, *largerP = next[0], next[1]
			} else {
				// Compared up to the end of the input only. Later input may
				// order the node's children differently, so drop them.
				*smallerP, *largerP = 0, 0
			}
			return mnum
		}
		if src[m+ml] < src[ip+ml] {
			*smallerP = uint32(matchIndex)
			cs = ml
			smallerP = &next[1]
			matchIndex = int32(next[1])
		} else {
			*largerP = uint32(matchIndex)
			cl = ml
			largerP = &next[0]
			matchIndex = int32(next[0])
		}
	}
	*smallerP, *largerP = 0, 0
	return mnum
}

// findHash3 inserts positions up to curr into the 3-byte hash table
// and returns the latest earlier position with the hash of curr.
func (e *ultraEnc) findHash3(curr int32) int32 {
	src := e.hist
	cur := e.cur
	t := &e.hashTable3
	for idx := e.next3; idx < curr; idx++ {
		t[ultraHash3(le.Load32(src, idx-cur))] = uint32(idx)
	}
	e.next3 = curr
	return int32(t[ultraHash3(le.Load32(src, curr-cur))])
}

func ultraHash4(u uint32) uint32 {
	return (u * prime4bytes) >> (32 - ultraHashLog)
}

func ultraHash3(u uint32) uint32 {
	return ((u << 8) * prime3bytes) >> (32 - ultraHash3Log)
}

// ultraCount returns the length of the common prefix of src[a:end] and src[b:].
// It requires b < a < end <= len(src).
func ultraCount(src []byte, a, b, end int32) int32 {
	if debugDeflate && (b < 0 || b >= a || a > end || int(end) > len(src)) {
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

// parseRegion parses hist[start:end] and writes it as blocks.
// The cache holds the matches of exactly these positions.
func (e *ultraEnc) parseRegion(w *huffmanBitWriter, start, end int32) {
	n := int32(end - start)
	if cap(e.cost) < int(n)+1 {
		e.cost = make([]uint32, n+1)
		e.choice = make([]uint32, n)
	}
	e.cost, e.choice = e.cost[:n+1], e.choice[:n]
	e.src = e.hist[start:end]
	e.base = start
	if e.defaultCosts() < n>>ultraFewShift || n < ultraSmall {
		// Hardly anything matches, or the region is small,
		// so more passes would not pay. Splitting still finds what to store.
		e.dp(0, n, &e.costs, maxMatchLength)
		bs := int32(0)
		for _, be := range e.splitBlocks(n) {
			e.emit(w, bs, be)
			bs = be
		}
		return
	}
	// Parse the region with one code to find where its statistics change.
	for range ultraSplitPasses {
		e.dp(0, n, &e.costs, ultraRegionSpan)
		e.countFreqs(0, n)
		e.buildCodes(&e.costs)
	}
	bs := int32(0)
	for _, be := range e.splitBlocks(n) {
		e.parseBlock(w, bs, be)
		bs = be
	}
}

// parseBlock parses src[bs:be], starting from the costs of the region parse
// of it, and writes it.
func (e *ultraEnc) parseBlock(w *huffmanBitWriter, bs, be int32) {
	e.countFreqs(bs, be)
	e.buildCodes(&e.costs)
	bestBits := math.MaxInt
	var next ultraCosts
	choice := e.choice[bs:be]
	for range ultraPasses {
		e.dp(bs, be, &e.costs, maxMatchLength)
		e.countFreqs(bs, be)
		b := e.buildCodes(&next)
		if b >= bestBits {
			// Go back to the best parse.
			copy(choice, e.bestChoice)
			break
		}
		gain := bestBits - b
		bestBits = b
		if next == e.costs || gain < b>>ultraGainShift {
			break
		}
		e.costs = next
		e.bestChoice = append(e.bestChoice[:0], choice...)
	}
	e.emit(w, bs, be)
}

// defaultCosts sets the costs for the first pass from a greedy parse of the cached matches.
// Like libdeflate, it takes only longer matches when few distinct literals are used,
// since short matches rarely pay off then.
func (e *ultraEnc) defaultCosts() (matched int32) {
	f := &e.freqs
	clear(f.lit[:])
	clear(f.off[:])
	src := e.src
	n := int32(len(src))
	for _, b := range src {
		f.lit[b]++
	}
	used := 0
	for _, v := range f.lit[:256] {
		if v > uint32(n>>11) {
			used++
		}
	}
	minLen := int32(ultraGreedyMin)
	if used < len(ultraMinLens) {
		minLen = max(minLen, int32(ultraMinLens[used]))
	}
	clear(f.lit[:256])
	cache := e.cache
	hdr := e.hdr
	for i := int32(0); i < n; {
		h := hdr[i]
		if cache[h] > 0 {
			m := cache[h-1]
			if l := min(int32(m>>16&0x1ff), n-i); l >= minLen {
				f.lit[lengthCodesStart+int(lengthCodes[l-baseMatchLength])]++
				f.off[m>>25]++
				e.choice[i] = uint32(l)<<16 | m&^(0x1ff<<16)
				i += l
				matched += l
				continue
			}
		}
		f.lit[src[i]]++
		e.choice[i] = 0
		i++
	}
	f.lit[endBlockMarker] = 1
	e.buildCodes(&e.costs)
	return matched
}

// dp finds the cheapest parse of src[bs:be] under c, from the end backwards.
// Matches are cut at be.
func (e *ultraEnc) dp(bs, be int32, c *ultraCosts, span int32) {
	cost := e.cost[:be+1]
	choice := e.choice[:be]
	src := e.src[:be]
	cache := e.cache
	hdr := e.hdr[:be]
	// Cheapest offset cost and its distance among the matches from each on.
	var om, od [maxMatchLength]uint32
	cost[be] = 0
	for i := be - 1; i >= bs; i-- {
		h := hdr[i]
		cnt := int32(cache[h])
		best := cost[i+1] + c.lit[src[i]]
		bc := uint32(0)
		if cnt > 0 {
			ms := cache[h-cnt : h]
			o, d := uint32(math.MaxUint32), uint32(0)
			for k := cnt - 1; k >= 0; k-- {
				x := ms[k] &^ (0x1ff << 16)
				if oc := c.off[x>>25]; oc < o {
					o, d = oc, x
				}
				om[k], od[k] = o, d
			}
			limit := be - i
			l := int32(baseMatchLength)
			ci := cost[i:]
			for k, m := range ms {
				ml := min(int32(m>>16&0x1ff), limit)
				if l > ml {
					continue
				}
				// Region passes only try the longest lengths of each match.
				l = max(l, ml-span)
				lcs := c.length[l : ml+1]
				cs := ci[l : ml+1]
				cs = cs[:len(lcs)]
				// The cheapest length; the shortest one on ties.
				mc, mj := uint32(math.MaxUint32), 0
				for j, lv := range lcs {
					if t := lv + cs[j]; t < mc {
						mc, mj = t, j
					}
				}
				if t := om[k] + mc; t < best {
					best = t
					bc = uint32(l+int32(mj))<<16 | od[k]
				}
				l = ml + 1
			}
		}
		cost[i] = best
		choice[i] = bc
	}
}

// countFreqs counts the symbols of the parse of src[bs:be].
func (e *ultraEnc) countFreqs(bs, be int32) {
	f := &e.freqs
	clear(f.lit[:])
	clear(f.off[:])
	src := e.src
	choice := e.choice
	for i := bs; i < be; {
		ch := choice[i]
		if ch == 0 {
			f.lit[src[i]]++
			i++
			continue
		}
		l := int32(ch >> 16 & 0x1ff)
		f.lit[lengthCodesStart+int(lengthCodes[l-baseMatchLength])]++
		f.off[ch>>25]++
		i += l
	}
	f.lit[endBlockMarker] = 1
}

const ultraSyms = literalCount + offsetCodeCount

// splitBlocks returns the ends of the blocks the region parse in e.choice
// is written as. Blocks start at units of ultraSplitUnit tokens.
func (e *ultraEnc) splitBlocks(n int32) []int32 {
	var cnt [ultraSyms]uint32
	pre := append(e.pre[:0], cnt[:]...)
	unitPos := append(e.unitPos[:0], 0)
	src := e.src
	choice := e.choice
	tokens := 0
	for i := int32(0); i < n; {
		if tokens == ultraSplitUnit {
			unitPos = append(unitPos, i)
			pre = append(pre, cnt[:]...)
			tokens = 0
		}
		tokens++
		ch := choice[i]
		if ch == 0 {
			cnt[src[i]]++
			i++
			continue
		}
		l := int32(ch >> 16 & 0x1ff)
		cnt[lengthCodesStart+int(lengthCodes[l-baseMatchLength])]++
		cnt[literalCount+int(ch>>25)]++
		i += l
	}
	unitPos = append(unitPos, n)
	pre = append(pre, cnt[:]...)
	e.pre, e.unitPos = pre, unitPos
	ends := e.ends[:0]
	e.split(0, len(unitPos)-1, &ends)
	e.ends = ends
	return ends
}

// split appends the ends of the blocks units a to b are written as.
func (e *ultraEnc) split(a, b int, ends *[]int32) {
	if b-a >= 2 {
		bestK, bestEst := 0, e.estimate(a, b)
		try := func(lo, hi, step int) {
			for k := max(lo, a+1); k < min(hi, b); k += step {
				if v := e.estimate(a, k) + e.estimate(k, b); v < bestEst {
					bestK, bestEst = k, v
				}
			}
		}
		// Look at most ultraSplitTries starts, then around the best of them.
		step := max(1, (b-a)/ultraSplitTries)
		try(a+step, b, step)
		if step > 1 && bestK != 0 {
			try(bestK-step+1, bestK+step, 1)
		}
		// Blocks must stay below the token limit.
		force := (b-a)*ultraSplitUnit > ultraBlockTokens
		if force && bestK == 0 {
			bestK = (a + b) / 2
		}
		if bestK != 0 && (force || e.exact(a, bestK)+e.exact(bestK, b) < e.exact(a, b)) {
			e.split(a, bestK, ends)
			e.split(bestK, b, ends)
			return
		}
	}
	*ends = append(*ends, e.unitPos[b])
}

// stored returns the size in bits of units a to b as stored blocks.
func (e *ultraEnc) stored(a, b int) int {
	n := int(e.unitPos[b] - e.unitPos[a])
	return 8*n + 40*((n+maxStoreBlockSize-1)/maxStoreBlockSize)
}

// estimate returns the approximate size in bits of units a to b as one block.
func (e *ultraEnc) estimate(a, b int) float64 {
	pa := e.pre[a*ultraSyms : (a+1)*ultraSyms]
	pb := e.pre[b*ultraSyms : (b+1)*ultraSyms]
	var bits float64
	used := 0
	for _, r := range [2][2]int{{0, literalCount}, {literalCount, ultraSyms}} {
		var total uint32
		var sum float64
		for s := r[0]; s < r[1]; s++ {
			if f := pb[s] - pa[s]; f > 0 {
				total += f
				sum += float64(f) * float64(mFastLog2(float32(f)))
				used++
			}
		}
		if total > 0 {
			bits += float64(total)*float64(mFastLog2(float32(total))) - sum
		}
	}
	// Extra bits don't depend on the split, but count against storing.
	var extra uint32
	for s := lengthCodesStart; s < literalCount; s++ {
		extra += (pb[s] - pa[s]) * uint32(lengthExtraBits[s-lengthCodesStart])
	}
	for s := range offsetCodeCount {
		extra += (pb[literalCount+s] - pa[literalCount+s]) * uint32(offsetExtraBits[s])
	}
	bits += ultraHeaderBase + ultraHeaderPerSym*float64(used) + float64(extra)
	return min(bits, float64(e.stored(a, b)))
}

// exact returns the size in bits of units a to b as one dynamic or stored block.
func (e *ultraEnc) exact(a, b int) int {
	pa := e.pre[a*ultraSyms : (a+1)*ultraSyms]
	pb := e.pre[b*ultraSyms : (b+1)*ultraSyms]
	f := &e.freqs
	for s := range f.lit {
		f.lit[s] = pb[s] - pa[s]
	}
	for s := range offsetCodeCount {
		f.off[s] = pb[literalCount+s] - pa[literalCount+s]
	}
	f.lit[endBlockMarker] = 1
	var c ultraCosts
	return min(e.buildCodes(&c), e.stored(a, b))
}

// buildCodes builds Huffman codes for e.freqs, sets c to the costs they give,
// and returns the size in bits of a dynamic block with them.
func (e *ultraEnc) buildCodes(c *ultraCosts) int {
	sw := e.sw
	f := &e.freqs
	var mx uint32
	for _, v := range f.lit {
		mx = max(mx, v)
	}
	for _, v := range f.off {
		mx = max(mx, v)
	}
	shift := 0
	for mx>>shift > math.MaxUint16 {
		shift++
	}
	scale := func(v uint32) uint16 {
		if v == 0 {
			return 0
		}
		return uint16(max(v>>shift, 1))
	}
	numLit := literalCount
	for i, v := range f.lit {
		sw.literalFreq[i] = scale(v)
	}
	for numLit > 257 && sw.literalFreq[numLit-1] == 0 {
		numLit--
	}
	numOff := offsetCodeCount
	for i, v := range f.off[:offsetCodeCount] {
		sw.offsetFreq[i] = scale(v)
	}
	for numOff > 0 && sw.offsetFreq[numOff-1] == 0 {
		numOff--
	}
	if numOff == 0 {
		sw.offsetFreq[0] = 1
		numOff = 1
	}
	sw.generate()
	sw.generateCodegen(numLit, numOff, sw.literalEncoding, sw.offsetEncoding)
	sw.codegenEncoding.generate(sw.codegenFreq[:], 7)
	size, _ := sw.headerSize()

	lc := sw.literalEncoding.codes
	for i, v := range f.lit[:literalCount] {
		size += int(v) * int(lc[i].len())
		if i >= lengthCodesStart {
			size += int(v) * int(lengthExtraBits[i-lengthCodesStart])
		}
	}
	oc := sw.offsetEncoding.codes
	for i, v := range f.off[:offsetCodeCount] {
		size += int(v) * (int(oc[i].len()) + int(offsetExtraBits[i]))
	}

	for i := range c.lit {
		c.lit[i] = ultraCodeCost(lc[i].len(), ultraLitNoStat)
	}
	for l := baseMatchLength; l <= maxMatchLength; l++ {
		code := lengthCodes[l-baseMatchLength]
		c.length[l] = ultraCodeCost(lc[lengthCodesStart+int(code)].len(), ultraLenNoStat) + uint32(lengthExtraBits[code])*ultraBit
	}
	for i := range c.off[:offsetCodeCount] {
		c.off[i] = ultraCodeCost(oc[i].len(), ultraOffNoStat) + uint32(offsetExtraBits[i])*ultraBit
	}
	return size
}

func ultraCodeCost(n uint8, noStat uint32) uint32 {
	if n == 0 {
		return noStat * ultraBit
	}
	return uint32(n) * ultraBit
}

// emit writes the parse of src[bs:be] as a block.
func (e *ultraEnc) emit(w *huffmanBitWriter, bs, be int32) {
	tok := &e.tok
	tok.Reset()
	src := e.src
	choice := e.choice
	for i := bs; i < be; {
		if tok.n >= ultraBlockTokens {
			w.writeBlockUltra(tok, false, src[bs:i])
			tok.Reset()
			bs = i
		}
		ch := choice[i]
		if ch == 0 {
			tok.AddLiteral(src[i])
			i++
			continue
		}
		l := int32(ch >> 16 & 0x1ff)
		if debugDeflate {
			e.checkMatch(i, l, ch&0x7fff)
		}
		tok.AddMatch(uint32(l-baseMatchLength), (ch&0x7fff)-minOffsetSize)
		i += l
	}
	if tok.n > 0 {
		w.writeBlockUltra(tok, false, src[bs:be])
		tok.Reset()
	}
}

// fillUltra copies input to the window, which is allocated on first use.
func (d *compressor) fillUltra(b []byte) int {
	if d.window == nil {
		d.window = make([]byte, ultraChunk)
	}
	if d.ultra == nil {
		d.ultra = newUltraEnc()
	}
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

// stepUltra encodes the window when it is full or when flushing.
func (d *compressor) stepUltra() {
	if d.ultra == nil || !d.sync && (d.windowEnd == 0 || d.windowEnd < len(d.window)) {
		return
	}
	d.ultra.encode(d.w, d.window[:d.windowEnd], d.sync)
	d.windowEnd = 0
	d.err = d.w.err
}

// ultraMinLens is libdeflate's shortest match worth taking in a greedy parse,
// by the number of distinct literals.
var ultraMinLens = [...]uint8{
	9, 9, 9, 9, 9, 9, 8, 8, 7, 7, 6, 6, 6, 6, 6, 6,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 4, 4, 4,
}

// ultraEntry packs a match: the distance in bits 0-14, the length in bits
// 16-24 and the offset code in bits 25-29.
func ultraEntry(length int32, dist uint32) uint32 {
	return uint32(length)<<16 | dist | offsetCode(dist-1)<<25
}

// checkMatch panics unless the match at index i of the region is valid.
func (e *ultraEnc) checkMatch(i, l int32, dist uint32) {
	p := e.base + i
	if dist == 0 || dist > ultraMaxDist || int32(dist) > p+e.cur-e.lowLimit || l > maxMatchLength {
		panic(fmt.Sprintf("invalid match at %d: length %d, distance %d", p, l, dist))
	}
	for k := range l {
		if e.hist[p+k] != e.hist[p+k-int32(dist)] {
			panic(fmt.Sprintf("match at %d, length %d, distance %d differs at %d", p, l, dist, k))
		}
	}
}
