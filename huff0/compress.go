package huff0

import (
	"fmt"
	"runtime"
	"sync"
)

func Compress(in []byte, s *Scratch) (out []byte, reUsed bool, err error) {
	if len(in) > BlockSizeMax {
		return nil, false, ErrIncompressible
	}
	s, err = s.prepare(in)
	if err != nil {
		return nil, false, err
	}
	if s.allowReuse {
		// TODO: See if we can reuse
	}

	// Create histogram, if none was provided.
	maxCount := s.maxCount
	var reuse = false
	if maxCount == 0 {
		maxCount, reuse = s.countSimple(in)
	}

	if false {
		reuse = s.canUseTable(s.prevTable)
	}
	// Place old table in prevtable

	// Reset for next run.
	s.clearCount = true
	s.maxCount = 0
	if maxCount == len(in) {
		// One symbol, use RLE
		return nil, false, ErrUseRLE
	}
	if maxCount == 1 || maxCount < (len(in)>>7) {
		// Each symbol present maximum once or too well distributed.
		return nil, false, ErrIncompressible
	}
	if reuse {
		// TODO:

	}
	s.optimalTableLog()
	err = s.buildCTable()
	if err != nil {
		return nil, false, err
	}
	s.cTable.write(s)
	if reuse && s.prevTable != nil {
		hSize := len(s.Out)
		oldSize := s.prevTable.estimateSize(s.count[:s.symbolLen])
		newSize := s.cTable.estimateSize(s.count[:s.symbolLen])
		if oldSize <= hSize+newSize || hSize+12 >= len(in) {
			// Remove header
			s.Out = s.Out[:0]
			// TODO:
			// compress using old table
			s.cTable = s.prevTable
			s.Out, err = s.compress1X(s.bw[0], s.Out, in)
			if len(s.Out) >= len(in) {
				return nil, false, ErrIncompressible
			}
			return s.Out, true, nil
		}
	}
	if true {
		if !s.canUseTable(s.cTable) {
			panic("invalid cTable")
		}
	}
	// Compress using new table
	if s.bw[0] == nil {
		s.bw[0] = &bitWriter{}
	}
	s.Out, err = s.compress1X(s.bw[0], s.Out, in)
	if len(s.Out) >= len(in) {
		return nil, false, ErrIncompressible
	}
	return s.Out, false, err
}

func (s *Scratch) compress1X(bw *bitWriter, dst, src []byte) ([]byte, error) {
	bw.reset(dst)
	// N is length divisible by 4.
	n := len(src)
	n -= n & 3
	cTable := s.cTable[:256]

	// encode a symbol
	encode := func(symbol byte) {
		enc := cTable[symbol]
		bw.addBits16Clean(enc.val, enc.nBits)
	}

	// Encode last bytes.
	for i := len(src) & 3; i > 0; i-- {
		encode(src[n+i-1])
	}

	if s.actualTableLog <= 8 {
		for ; n > 0; n -= 4 {
			v3, v2, v1, v0 := src[n-4], src[n-3], src[n-2], src[n-1]
			bw.flush32()
			encode(v0)
			encode(v1)
			encode(v2)
			encode(v3)
		}
	} else {
		for ; n > 0; n -= 4 {
			v3, v2, v1, v0 := src[n-4], src[n-3], src[n-2], src[n-1]
			bw.flush32()
			encode(v0)
			encode(v1)
			bw.flush32()
			encode(v2)
			encode(v3)
		}
	}
	err := bw.close()
	return bw.out, err
}

func (s *Scratch) compress4X(src []byte) ([]byte, error) {
	if len(src) < 12 {
		return nil, ErrIncompressible
	}
	segmentSize := (len(src) + 3) / 4
	var wg sync.WaitGroup
	var errs [4]error
	wg.Add(4)
	for i := 0; i < 4; i++ {
		if s.bw[i] == nil {
			s.bw[i] = &bitWriter{}
		}
		toDo := src
		if len(toDo) > segmentSize {
			toDo = toDo[:segmentSize]
		}
		src = src[len(toDo):]

		// If one one processor, output directly.
		if runtime.GOMAXPROCS(0) == 1 {
			var err error
			// Add placeholder for length.
			idx := len(s.Out)
			s.Out = append(s.Out, 0, 0)
			s.Out, err = s.compress1X(s.bw[i], s.Out, toDo)
			if err != nil {
				return nil, err
			}
			// Write compressed length as little endian before block.
			length := len(s.Out) - idx - 2
			s.Out[idx] = byte(length)
			s.Out[idx+1] = byte(length >> 8)
			continue
		}
		// Separate goroutine for each block.
		go func(i int) {
			s.tmpOut[i], errs[i] = s.compress1X(s.bw[i], s.tmpOut[i][:0], toDo)
			wg.Done()
		}(i)
	}
	if runtime.GOMAXPROCS(0) == 1 {
		return s.Out, nil
	}
	wg.Wait()
	for i := 0; i < 4; i++ {
		if errs[i] != nil {
			return nil, errs[i]
		}
		o := s.tmpOut[i]
		// Write compressed length as little endian.
		s.Out = append(s.Out, byte(len(o)), byte(len(o)>>8))
		// Write output.
		s.Out = append(s.Out, o...)
	}
	return s.Out, nil
}

// countSimple will create a simple histogram in s.count.
// Returns the biggest count.
// Does not update s.clearCount.
func (s *Scratch) countSimple(in []byte) (max int, reuse bool) {
	reuse = true
	for _, v := range in {
		s.count[v]++
	}
	m := uint32(0)
	if len(s.cTable) > 0 {
		for i, v := range s.count[:] {
			if v > m {
				m = v
			}
			if v > 0 {
				s.symbolLen = uint16(i) + 1
				if i >= len(s.cTable) {
					reuse = false
				} else {
					if s.cTable[i].nBits == 0 {
						reuse = false
					}
				}
			}
		}
		return int(m), reuse
	}
	for i, v := range s.count[:] {
		if v > m {
			m = v
		}
		if v > 0 {
			s.symbolLen = uint16(i) + 1
		}
	}
	return int(m), false
}

func (s *Scratch) canUseTable(c cTable) bool {
	if len(c) < int(s.symbolLen) {
		return false
	}
	for i, v := range s.count[:s.symbolLen] {
		if v != 0 && c[i].nBits == 0 {
			return false
		}
	}
	return true
}

// minTableLog provides the minimum logSize to safely represent a distribution.
func (s *Scratch) minTableLog() uint8 {
	minBitsSrc := highBits(uint32(s.br.remain()-1)) + 1
	minBitsSymbols := highBits(uint32(s.symbolLen-1)) + 2
	if minBitsSrc < minBitsSymbols {
		return uint8(minBitsSrc)
	}
	return uint8(minBitsSymbols)
}

// optimalTableLog calculates and sets the optimal tableLog in s.actualTableLog
func (s *Scratch) optimalTableLog() {
	tableLog := s.TableLog
	minBits := s.minTableLog()
	maxBitsSrc := uint8(highBits(uint32(s.br.remain()-1))) - 2
	if maxBitsSrc < tableLog {
		// Accuracy can be reduced
		tableLog = maxBitsSrc
	}
	if minBits > tableLog {
		tableLog = minBits
	}
	// Need a minimum to safely represent all symbol values
	if tableLog < minTablelog {
		tableLog = minTablelog
	}
	if tableLog > tableLogMax {
		tableLog = tableLogMax
	}
	s.actualTableLog = tableLog
}

type cTableEntry struct {
	val   uint16
	nBits uint8
	// We have 8 bits extra
}

const huffNodesMask = huffNodesLen - 1

func (s *Scratch) buildCTable() error {
	s.huffSort()
	s.cTable = s.cTable[:s.symbolLen]

	var startNode = int16(s.symbolLen)
	nonNullRank := s.symbolLen - 1

	for s.nodes[nonNullRank].count == 0 {
		nonNullRank--
	}
	//FIXME: this relies on indexing "-1". Go doesn't allow that.
	nodeNb := int16(startNode)
	huffNode := s.nodes[1 : huffNodesLen+1]

	// This overlays the slice above, but allows "-1" index lookups.
	// Different from reference implementation.
	huffNode0 := s.nodes[0 : huffNodesLen+1]

	lowS := int16(nonNullRank)
	nodeRoot := nodeNb + lowS - 1
	lowN := nodeNb
	huffNode[nodeNb].count = huffNode[lowS].count + huffNode[lowS-1].count
	huffNode[lowS].parent, huffNode[lowS-1].parent = uint16(nodeNb), uint16(nodeNb)
	nodeNb++
	lowS -= 2
	for n := nodeNb; n <= nodeRoot; n++ {
		huffNode[n].count = 1 << 30
	}
	// fake entry, strong barrier
	huffNode0[0].count = 1 << 31

	// create parents
	for nodeNb <= nodeRoot {
		var n1, n2 int16
		if huffNode0[lowS+1].count < huffNode0[lowN+1].count {
			n1 = lowS
			lowS--
		} else {
			n1 = lowN
			lowN++
		}
		if huffNode0[lowS+1].count < huffNode0[lowN+1].count {
			n2 = lowS
			lowS--
		} else {
			n2 = lowN
			lowN++
		}

		huffNode[nodeNb].count = huffNode0[n1+1].count + huffNode0[n2+1].count
		huffNode0[n1+1].parent, huffNode0[n2+1].parent = uint16(nodeNb), uint16(nodeNb)
		nodeNb++
	}

	// distribute weights (unlimited tree height)
	huffNode[nodeRoot].nbBits = 0
	for n := nodeRoot - 1; n >= startNode; n-- {
		huffNode[n].nbBits = huffNode[huffNode[n].parent].nbBits + 1
	}
	for n := uint16(0); n <= nonNullRank; n++ {
		huffNode[n].nbBits = huffNode[huffNode[n].parent].nbBits + 1
	}
	s.actualTableLog = s.setMaxHeight(int(nonNullRank))
	maxNbBits := s.actualTableLog

	// fill result into tree (val, nbBits)
	if maxNbBits > tableLogMax {
		return fmt.Errorf("internal error: maxNbBits (%d) > tableLogMax (%d)", maxNbBits, tableLogMax)
	}
	var nbPerRank [tableLogMax + 1]uint16
	var valPerRank [tableLogMax + 1]uint16
	for _, v := range huffNode[:nonNullRank] {
		nbPerRank[v.nbBits]++
	}
	// determine stating value per rank
	{
		min := uint16(0)
		for n := maxNbBits; n > 0; n-- {
			// get starting value within each rank
			valPerRank[n] = min
			min += nbPerRank[n]
			min >>= 1
		}
	}

	// push nbBits per symbol, symbol order
	for _, v := range huffNode[:s.symbolLen] {
		s.cTable[v.symbol].nBits = v.nbBits
	}

	// assign value within rank, symbol order
	for n := range s.cTable[:s.symbolLen] {
		v := valPerRank[s.cTable[n].nBits]
		s.cTable[n].val = v + 1
	}

	return nil
}

// huffSort will sort symbols, decreasing order.
func (s *Scratch) huffSort() {
	type rankPos struct {
		base    uint32
		current uint32
	}

	// Clear nodes
	nodes := s.nodes[:huffNodesLen+1]
	s.nodes = nodes
	nodes = nodes[1 : huffNodesLen+1]

	// Sort into buckets based on length of symbol count.
	var rank [32]rankPos
	for _, v := range s.count[:s.symbolLen] {
		r := highBits(v+1) & 31
		rank[r].base++
	}
	for n := 30; n > 0; n-- {
		rank[n-1].base += rank[n].base
	}
	for n := range rank[:] {
		rank[n].current = rank[n].base
	}
	for n, c := range s.count[:s.symbolLen] {
		r := highBits(c+1) + 1
		pos := rank[r].current
		rank[r].current++
		prev := nodes[(pos-1)&huffNodesMask]
		base := rank[r].base
		for (pos > base) && (c > prev.count) {
			nodes[pos&huffNodesMask] = prev
			pos--
		}
		nodes[pos&huffNodesMask] = nodeElt{count: c, symbol: byte(n)}
	}
}

func (s *Scratch) setMaxHeight(lastNonNull int) uint8 {
	maxNbBits := s.TableLog
	huffNode := s.nodes[1 : huffNodesLen+1]
	//huffNode = huffNode[: huffNodesLen]

	largestBits := huffNode[lastNonNull].nbBits

	// early exit : no elt > maxNbBits
	if largestBits <= maxNbBits {
		return largestBits
	}
	totalCost := int(0)
	baseCost := int(1) << (largestBits - maxNbBits)
	n := uint32(lastNonNull)

	for huffNode[n].nbBits > maxNbBits {
		totalCost += baseCost - (1 << (largestBits - huffNode[n].nbBits))
		huffNode[n].nbBits = maxNbBits
		n--
	}
	// n stops at huffNode[n].nbBits <= maxNbBits

	for huffNode[n].nbBits == maxNbBits {
		n--
	}
	// n end at index of smallest symbol using < maxNbBits

	// renorm totalCost
	totalCost >>= largestBits - maxNbBits /* note : totalCost is necessarily a multiple of baseCost */

	/* repay normalized cost */
	{
		const noSymbol = 0xF0F0F0F0
		var rankLast [tableLogMax + 2]uint32

		for i := range rankLast[:] {
			rankLast[i] = noSymbol
		}

		// Get pos of last (smallest) symbol per rank
		{
			currentNbBits := uint8(maxNbBits)
			for pos := int(n); pos >= 0; pos-- {
				if huffNode[pos].nbBits >= currentNbBits {
					continue
				}
				currentNbBits = huffNode[pos].nbBits /* < maxNbBits */
				rankLast[maxNbBits-currentNbBits] = uint32(pos)
			}
		}

		for totalCost > 0 {
			nBitsToDecrease := uint8(highBits(uint32(totalCost))) + 1

			for ; nBitsToDecrease > 1; nBitsToDecrease-- {
				highPos := rankLast[nBitsToDecrease]
				lowPos := rankLast[nBitsToDecrease-1]
				if highPos == noSymbol {
					continue
				}
				if lowPos == noSymbol {
					break
				}
				highTotal := huffNode[highPos].count
				lowTotal := 2 * huffNode[lowPos].count
				if highTotal <= lowTotal {
					break
				}
			}
			// only triggered when no more rank 1 symbol left => find closest one (note : there is necessarily at least one !)
			// HUF_MAX_TABLELOG test just to please gcc 5+; but it should not be necessary
			// FIXME: try to remove
			for (nBitsToDecrease <= tableLogMax) && (rankLast[nBitsToDecrease] == noSymbol) {
				nBitsToDecrease++
			}
			totalCost -= 1 << (nBitsToDecrease - 1)
			if rankLast[nBitsToDecrease-1] == noSymbol {
				// this rank is no longer empty
				rankLast[nBitsToDecrease-1] = rankLast[nBitsToDecrease]
			}
			huffNode[rankLast[nBitsToDecrease]].nbBits++
			if rankLast[nBitsToDecrease] == 0 {
				/* special case, reached largest symbol */
				rankLast[nBitsToDecrease] = noSymbol
			} else {
				rankLast[nBitsToDecrease]--
				if huffNode[rankLast[nBitsToDecrease]].nbBits != maxNbBits-nBitsToDecrease {
					rankLast[nBitsToDecrease] = noSymbol /* this rank is now empty */
				}
			}
		}

		for totalCost < 0 { /* Sometimes, cost correction overshoot */
			if rankLast[1] == noSymbol { /* special case : no rank 1 symbol (using maxNbBits-1); let's create one from largest rank 0 (using maxNbBits) */
				for huffNode[n].nbBits == maxNbBits {
					n--
				}
				huffNode[n+1].nbBits--
				rankLast[1] = n + 1
				totalCost++
				continue
			}
			huffNode[rankLast[1]+1].nbBits--
			rankLast[1]++
			totalCost++
		}
	}
	return maxNbBits
}

type nodeElt struct {
	count  uint32
	parent uint16
	symbol byte
	nbBits uint8
}
