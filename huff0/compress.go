package huff0

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

// huffSort will sort symbols, decreasing order.
func (s *Scratch) huffSort() {
	type rankPos struct {
		base    uint32
		current uint32
	}

	// Clear nodes
	s.nodes = s.nodes[:512]

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
		for (pos > rank[r].base) && (c > s.nodes[pos-1].count) {
			s.nodes[pos] = s.nodes[pos-1]
			pos--
		}
		s.nodes[pos] = nodeElt{count: c, symbol: byte(n)}
	}

}

type nodeElt struct {
	count  uint32
	parent uint16
	symbol byte
	nbBits uint8
}
