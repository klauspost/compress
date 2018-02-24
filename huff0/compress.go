package huff0

import "fmt"

func Compress(in []byte, s *Scratch) ([]byte, error) {
	var err error
	s, err = s.prepare(in)
	if err != nil {
		return nil, err
	}
	if s.allowReuse {
		// TODO: See if we can reuse
	}

	// Create histogram, if none was provided.
	maxCount := s.maxCount
	var reuse = false
	if maxCount == 0 {
		maxCount, reuse = s.countSimple(in)
	} else {
		reuse = s.canReuseCTable()
	}
	// Reset for next run.
	s.clearCount = true
	s.maxCount = 0
	if maxCount == len(in) {
		// One symbol, use RLE
		return nil, ErrUseRLE
	}
	if maxCount == 1 || maxCount < (len(in)>>7) {
		// Each symbol present maximum once or too well distributed.
		return nil, ErrIncompressible
	}
	if reuse {
		// TODO:
	}
	s.optimalTableLog()
	err = s.buildCTable()
	if err != nil {
		return nil, err
	}

	return nil, nil
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

func (s *Scratch) canReuseCTable() bool {
	if len(s.cTable) < int(s.symbolLen) {
		return false
	}
	for i, v := range s.count[:s.symbolLen] {
		if v != 0 && s.cTable[i].nBits == 0 {
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

	var startNode = s.symbolLen
	nonNullRank := s.symbolLen - 1

	for s.nodes[nonNullRank].count == 0 {
		nonNullRank--
	}
	nodeNb := uint16(startNode)
	huffNode := s.nodes[1:huffNodesLen]
	huffNode0 := &s.nodes[0]

	lowS := uint16(nonNullRank)
	nodeRoot := nodeNb + lowS - 1
	lowN := nodeNb
	huffNode[nodeNb].count = huffNode[lowS].count + huffNode[lowS-1].count
	huffNode[lowS].parent, huffNode[lowS-1].parent = nodeNb, nodeNb
	nodeNb++
	lowS -= 2
	for n := nodeNb; n <= nodeRoot; n++ {
		huffNode[n].count = 1 << 30
	}
	// fake entry, strong barrier
	huffNode0.count = 1 << 31

	// create parents
	for nodeNb <= nodeRoot {
		var n1, n2 uint16
		if huffNode[lowS].count < huffNode[lowN].count {
			n1 = lowS
			lowS--
		} else {
			n1 = lowN
			lowN++
		}
		if huffNode[lowS].count < huffNode[lowN].count {
			n2 = lowS
			lowS--
		} else {
			n2 = lowN
			lowN++
		}

		huffNode[nodeNb].count = huffNode[n1].count + huffNode[n2].count
		huffNode[n1].parent, huffNode[n2].parent = nodeNb, nodeNb
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
		for (pos > rank[r].base) && (c > prev.count) {
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
