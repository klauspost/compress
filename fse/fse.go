package fse

import (
	"fmt"
	"github.com/pkg/errors"
	"math"
	"math/bits"
)

const (
	/*!MEMORY_USAGE :
	 *  Memory usage formula : N->2^N Bytes (examples : 10 -> 1KB; 12 -> 4KB ; 16 -> 64KB; 20 -> 1MB; etc.)
	 *  Increasing memory usage improves compression ratio
	 *  Reduced memory usage can improve speed, due to cache effect
	 *  Recommended max value is 14, for 16KB, which nicely fits into Intel x86 L1 cache */
	maxMemoryUsage     = 14
	defaultMemoryUsage = 13

	maxTableLog      = maxMemoryUsage - 2
	maxTablesize     = 1 << maxTableLog
	maxtablesizeMask = maxTablesize - 1
	defaultTablelog  = defaultMemoryUsage - 2
	minTablelog      = 5
	maxSymbolValue   = 255
	ctableSize       = 1 + (1 << (maxTableLog - 1)) + (maxSymbolValue + 1)
)

type Scratch struct {
	// Private
	count           [maxSymbolValue + 1]uint32
	norm            [maxSymbolValue + 1]int16
	cTable          []uint32 // May contain values from last run.
	clearCount      bool     // clear count
	length          int      // input length
	actualMaxSymbol uint8
	actualTableLog  uint8

	// Out is output buffer
	Out []byte

	// Per block parameters
	MaxSymbolValue uint8
	TableLog       uint8
}

func (s *Scratch) prepare(in []byte) (*Scratch, error) {
	if s == nil {
		s = &Scratch{}
	}
	s.length = len(in)
	if s.MaxSymbolValue == 0 {
		s.MaxSymbolValue = 255
	}
	if s.TableLog == 0 {
		s.TableLog = defaultTablelog
	}
	if s.TableLog > maxTableLog {
		return nil, fmt.Errorf("tableLog (%d) > maxTableLog (%d)", s.TableLog, maxTableLog)
	}
	if cap(s.Out) == 0 {
		s.Out = make([]byte, 0, len(in))
	}
	if s.clearCount {
		for i := range s.count {
			s.count[i] = 0
		}
		s.clearCount = false
	}
	cTableSize := 1 + (1 << (uint(s.TableLog) - 1)) + ((int(s.MaxSymbolValue) + 1) * 2)
	if cap(s.cTable) < cTableSize {
		s.cTable = make([]uint32, 0, cTableSize)
	}
	s.cTable = s.cTable[:cTableSize]
	return s, nil
}

// countSimple will create a simple histogram in s.count
// Returns the biggest count.
func (s *Scratch) countSimple(in []byte) (max int) {
	s.clearCount = true
	for _, v := range in {
		s.count[v]++
	}
	m := uint32(0)
	for i, v := range s.count[:] {
		if v > m {
			m = v
		}
		if v > 0 {
			s.actualMaxSymbol = uint8(i)
		}
	}
	return int(m)
}

// minTableLog provides the minimum logSize to safely represent a distribution.
func (s *Scratch) minTableLog() uint8 {
	minBitsSrc := bits.Len32(uint32(s.length-1)) + 1
	minBitsSymbols := bits.Len32(uint32(s.actualMaxSymbol)) + 2
	if minBitsSrc < minBitsSymbols {
		return uint8(minBitsSrc)
	}
	return uint8(minBitsSymbols)
}

// optimalTableLog calculates and sets the optimal tableLog in s.actualTableLog
func (s *Scratch) optimalTableLog() {
	tableLog := s.TableLog
	minBits := s.minTableLog()
	maxBitsSrc := uint8(bits.Len32(uint32(s.length - 2)))
	if maxBitsSrc < s.actualTableLog {
		// Accuracy can be reduced
		s.actualTableLog = maxBitsSrc
	}
	if minBits > tableLog {
		tableLog = minBits
	}
	/* Need a minimum to safely represent all symbol values */
	if tableLog < minTablelog {
		tableLog = minTablelog
	}
	if tableLog > maxTableLog {
		tableLog = maxTableLog
	}
	s.actualTableLog = tableLog
}

var rtbTable = [...]uint32{0, 473195, 504333, 520860, 550000, 700000, 750000, 830000}

func (s *Scratch) normalizeCount() error {
	tableLog := s.actualTableLog

	var (
		scale             = 62 - uint64(tableLog)
		step              = (1 << 62) / uint64(s.length)
		vStep             = uint64(1) << (scale - 20)
		stillToDistribute = int16(1 << tableLog)
		largest           int
		largestP          int16
		lowThreshold      = (uint32)(s.length >> tableLog)
	)

	for i, cnt := range s.count[:s.actualMaxSymbol] {
		// already handled
		// if (count[s] == s.length) return 0;   /* rle special case */

		if cnt == 0 {
			s.norm[i] = 0
			continue
		}
		if cnt <= lowThreshold {
			s.norm[i] = -1
			stillToDistribute--
		} else {
			proba := (int16)((uint64(cnt) * step) >> scale)
			if proba < 8 {
				restToBeat := vStep * uint64(rtbTable[proba])
				v := uint64(cnt)*step - (uint64(proba) << scale)
				if v > restToBeat {
					proba++
				}
			}
			if proba > largestP {
				largestP = proba
				largest = i
			}
			s.norm[i] = proba
			stillToDistribute -= proba
		}
	}

	if -stillToDistribute >= (s.norm[largest] >> 1) {
		// corner case, need another normalization method
		// FIXME:
		//return s.normalizeM2()
	} else {
		s.norm[largest] += stillToDistribute
	}
	// TODO: Print stuff
	return nil
}

// Secondary normalization method.
// To be used when primary method fails.
func (s *Scratch) normalizeCount2() error {

	const NOT_YET_ASSIGNED = -2
	var (
		distributed  uint32
		toDistribute uint32

		// Init
		total        = uint32(s.length)
		tableLog     = s.actualTableLog
		lowThreshold = uint32(total >> tableLog)
		lowOne       = uint32((total * 3) >> (tableLog + 1))
	)
	for i, cnt := range s.count[:s.actualMaxSymbol] {
		if cnt == 0 {
			s.norm[i] = 0
			continue
		}
		if cnt <= lowThreshold {
			s.norm[i] = -1
			distributed++
			total -= cnt
			continue
		}
		if cnt <= lowOne {
			s.norm[i] = 1
			distributed++
			total -= cnt
			continue
		}
		s.norm[i] = NOT_YET_ASSIGNED
	}
	toDistribute = (1 << tableLog) - distributed

	if (total / toDistribute) > lowOne {
		// risk of rounding to zero
		lowOne = uint32((total * 3) / (toDistribute * 2))
		for i, cnt := range s.count[:s.actualMaxSymbol] {
			if (s.norm[i] == NOT_YET_ASSIGNED) && (cnt <= lowOne) {
				s.norm[i] = 1
				distributed++
				total -= cnt
				continue
			}
		}
		toDistribute = (1 << tableLog) - distributed
	}
	if distributed == uint32(s.actualMaxSymbol)+1 {
		// all values are pretty poor;
		//   probably incompressible data (should have already been detected);
		//   find max, then give all remaining points to max
		var maxV int
		var maxC uint32
		for i, cnt := range s.count[:s.actualMaxSymbol] {
			if cnt > maxC {
				maxV = i
				maxC = cnt
			}
		}
		s.norm[maxV] += int16(toDistribute)
		return nil
	}

	if total == 0 {
		// all of the symbols were low enough for the lowOne or lowThreshold
		for i := uint32(0); toDistribute > 0; i = (i + 1) % (uint32(s.actualMaxSymbol) + 1) {
			if s.norm[i] > 0 {
				toDistribute--
				s.norm[i]++
			}
		}
		return nil
	}

	var (
		vStepLog = 62 - uint64(tableLog)
		mid      = uint64((1 << (vStepLog - 1)) - 1)
		rStep    = (((1 << vStepLog) * uint64(toDistribute)) + mid) / uint64(total) // scale on remaining
		tmpTotal = mid
	)
	for i, cnt := range s.count[:s.actualMaxSymbol] {
		if s.norm[i] == NOT_YET_ASSIGNED {
			var (
				end    = tmpTotal + uint64(cnt)*rStep
				sStart = uint32(tmpTotal >> vStepLog)
				sEnd   = uint32(end >> vStepLog)
				weight = sEnd - sStart
			)
			if weight < 1 {
				return errors.New("weight < 1")
			}
			s.norm[i] = int16(weight)
			tmpTotal = end
		}
	}
	return nil
}

func Compress(in []byte, s *Scratch) ([]byte, error) {
	if len(in) <= 1 {
		return nil, nil
	}
	if len(in) > math.MaxUint32 {
		return nil, errors.New("input too big, must be < 2GB")
	}
	s, err := s.prepare(in)
	if err != nil {
		return nil, err
	}

	// Create histogram
	maxCount := s.countSimple(in)
	if maxCount == len(in) {
		// One symbol, use RLE
	}
	if maxCount == 1 || maxCount < (len(in)>>7) {
		// Each symbol present maximum once or too well distributed.
		// Uncompressible.
		return nil, nil
	}

	return nil, nil
}
