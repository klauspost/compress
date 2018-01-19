package fse

import "errors"

const (
	tablelogAbsoluteMax = 15
)

func (s *Scratch) readNCount(b byteReader) error {
	var (
		charnum   byte
		previous0 bool
	)

	if s.length < 4 {
		return errors.New("input too small")
	}
	bitStream := b.Uint32()
	nbBits := uint((bitStream & 0xF) + minTablelog) // extract tableLog
	if nbBits > tablelogAbsoluteMax {
		return errors.New("tableLog too large")
	}
	bitStream >>= 4
	bitCount := uint(4)
	s.actualTableLog = uint8(nbBits)
	remaining := uint32((1 << nbBits) + 1)
	threshold := uint32(1 << nbBits)
	nbBits++
	maxSVPtr := uint8(maxSymbolValue)

	for (remaining > 1) && (charnum < maxSVPtr) {
		if previous0 {
			n0 := charnum
			for (bitStream & 0xFFFF) == 0xFFFF {
				n0 += 24
				if b.remain() >= 4 {
					bitStream = b.Uint32Back2() >> bitCount
				} else {
					bitStream >>= 16
					bitCount += 16
				}
			}
			for (bitStream & 3) == 3 {
				n0 += 3
				bitStream >>= 2
				bitCount += 2
			}
			n0 += byte(bitStream & 3)
			bitCount += 2
			if n0 > maxSVPtr {
				return errors.New("maxSymbolValue too small")
			}
			for charnum < n0 {
				s.norm[charnum] = 0
				charnum++
			}

			if r := b.remain(); r >= 7 || (r+int(bitCount>>3) >= 4) {
				n := int(bitCount >> 3)
				bitCount &= 7
				bitStream = b.Uint32BackN(n) >> bitCount
			} else {
				bitStream >>= 2
			}
		}

		max := (2*threshold - 1) - remaining
		var count uint32

		if (bitStream & (threshold - 1)) < uint32(max) {
			count = bitStream & (threshold - 1)
			bitCount += nbBits - 1
		} else {
			count = bitStream & (2*threshold - 1)
			if count >= threshold {
				count -= max
			}
			bitCount += nbBits
		}

		count-- // extra accuracy
		if count < 0 {
			remaining = count
		} else {
			remaining -= count
		}
		s.norm[charnum] = int16(count)
		charnum++
		previous0 = count != 0
		for remaining < threshold {
			nbBits--
			threshold >>= 1
		}
		if r := b.remain(); r > 7 || (r+int(bitCount>>3) > 4) {
			ip += bitCount >> 3
			bitCount &= 7
		} else {
			bitCount -= (int)(8 * (iend - 4 - ip))
			ip = iend - 4
		}

		bitStream = MEM_readLE32(ip) >> (bitCount & 31)
	}
	if remaining != 1 {
		return errors.New("corruption_detected")
	}
	if bitCount > 32 {
		return errors.New("corruption_detected")
	}
	s.symbolLen = uint16(charnum)

	// offset output?
	//ip += (bitCount+7)>>3;

	/*
		size_t FSE_readNCount (short* normalizedCounter, unsigned* maxSVPtr, unsigned* tableLogPtr,
		                 const void* headerBuffer, size_t hbSize)
		{
		    const BYTE* const istart = (const BYTE*) headerBuffer;
		    const BYTE* const iend = istart + hbSize;
		    const BYTE* ip = istart;
		    int nbBits;
		    int remaining;
		    int threshold;
		    U32 bitStream;
		    int bitCount;
		    unsigned charnum = 0;
		    int previous0 = 0;

		    if (hbSize < 4) return ERROR(srcSize_wrong);
		    bitStream = MEM_readLE32(ip);
		    nbBits = (bitStream & 0xF) + FSE_MIN_TABLELOG;   // extract tableLog
		    if (nbBits > FSE_TABLELOG_ABSOLUTE_MAX) return ERROR(tableLog_tooLarge);
		    bitStream >>= 4;
		    bitCount = 4;
		    *tableLogPtr = nbBits;
		    remaining = (1<<nbBits)+1;
		    threshold = 1<<nbBits;
		    nbBits++;

		    while ((remaining>1) & (charnum<=*maxSVPtr)) {
		        if (previous0) {
		            unsigned n0 = charnum;
		            while ((bitStream & 0xFFFF) == 0xFFFF) {
		                n0 += 24;
		                if (ip < iend-5) {
		                    ip += 2;
		                    bitStream = MEM_readLE32(ip) >> bitCount;
		                } else {
		                    bitStream >>= 16;
		                    bitCount   += 16;
		            }   }
		            while ((bitStream & 3) == 3) {
		                n0 += 3;
		                bitStream >>= 2;
		                bitCount += 2;
		            }
		            n0 += bitStream & 3;
		            bitCount += 2;
		            if (n0 > *maxSVPtr) return ERROR(maxSymbolValue_tooSmall);
		            while (charnum < n0) normalizedCounter[charnum++] = 0;
		            if ((ip <= iend-7) || (ip + (bitCount>>3) <= iend-4)) {
		                ip += bitCount>>3;
		                bitCount &= 7;
		                bitStream = MEM_readLE32(ip) >> bitCount;
		            } else {
		                bitStream >>= 2;
		        }   }
		        {   int const max = (2*threshold-1) - remaining;
		            int count;

		            if ((bitStream & (threshold-1)) < (U32)max) {
		                count = bitStream & (threshold-1);
		                bitCount += nbBits-1;
		            } else {
		                count = bitStream & (2*threshold-1);
		                if (count >= threshold) count -= max;
		                bitCount += nbBits;
		            }

		            count--;   // extra accuracy
		            remaining -= count < 0 ? -count : count;   // -1 means +1
		            normalizedCounter[charnum++] = (short)count;
		            previous0 = !count;
		            while (remaining < threshold) {
		                nbBits--;
		                threshold >>= 1;
		            }

		            if ((ip <= iend-7) || (ip + (bitCount>>3) <= iend-4)) {
		                ip += bitCount>>3;
		                bitCount &= 7;
		            } else {
		                bitCount -= (int)(8 * (iend - 4 - ip));
		                ip = iend - 4;
		            }
		            bitStream = MEM_readLE32(ip) >> (bitCount & 31);
		    }   }   // while ((remaining>1) & (charnum<=*maxSVPtr))
		    if (remaining != 1) return ERROR(corruption_detected);
		    if (bitCount > 32) return ERROR(corruption_detected);
		    *maxSVPtr = charnum-1;

		    ip += (bitCount+7)>>3;
		    return ip-istart;
		}
	*/
	return nil
}

func Decompress(b []byte, s *Scratch) ([]byte, error) {
	s.prepare(b)
	br := byteReader{b: b}
	return nil, s.readNCount(br)
}
