package zstd

import (
	"fmt"
	"math/bits"

	"github.com/klauspost/compress/zstd/internal/xxhash"
)

const (
	dictShardBits = 7
)

type fastBase struct {
	// cur is the offset at the start of hist
	cur int32
	// maximum offset. Should be at least 2x block size.
	maxMatchOff int32
	bufferReset int32
	hist        []byte
	// own is our history buffer while hist is the caller's (see inPlace).
	own      []byte
	inp      bool
	crc      *xxhash.Digest
	tmp      [8]byte
	blk      *blockEnc
	lastDict *dict
	lowMem   bool
}

// CRC returns the underlying CRC writer.
func (e *fastBase) CRC() *xxhash.Digest {
	return e.crc
}

// AppendCRC will append the CRC to the destination slice and return it.
func (e *fastBase) AppendCRC(dst []byte) []byte {
	crc := e.crc.Sum(e.tmp[:0])
	dst = append(dst, crc[7], crc[6], crc[5], crc[4])
	return dst
}

// WindowSize returns the window size of the encoder,
// or a window size small enough to contain the input size, if > 0.
func (e *fastBase) WindowSize(size int64) int32 {
	if size > 0 && size < int64(e.maxMatchOff) {
		b := max(
			// Keep minimum window.
			int32(1)<<uint(bits.Len(uint(size))), 1024)
		return b
	}
	return e.maxMatchOff
}

// Block returns the current block.
func (e *fastBase) Block() *blockEnc {
	return e.blk
}

// inPlace encodes the following blocks where they are, instead of copying
// them into hist. b is the history, and the blocks must follow it within
// cap(b). They must not change until inPlace(nil) or a reset.
func (e *fastBase) inPlace(b []byte) {
	if e.inp {
		// Continue positions after b, so its table entries leave the window.
		e.cur += int32(len(e.hist))
		e.hist, e.own, e.inp = e.own, nil, false
	}
	if b != nil {
		e.own, e.hist, e.inp = e.hist[:0], b, true
	}
}

func (e *fastBase) addBlock(src []byte) int32 {
	if e.inp {
		s := int32(len(e.hist))
		e.hist = e.hist[:int(s)+len(src)]
		if len(src) > 0 && &e.hist[s] != &src[0] {
			panic("zstd: block does not follow its history")
		}
		return s
	}
	if debugAsserts && e.cur > e.bufferReset {
		panic(fmt.Sprintf("ecur (%d) > buffer reset (%d)", e.cur, e.bufferReset))
	}
	// check if we have space already
	if len(e.hist)+len(src) > cap(e.hist) {
		if cap(e.hist) == 0 {
			e.ensureHist(len(src))
		} else {
			if cap(e.hist) < int(e.maxMatchOff+maxCompressedBlockSize) {
				panic(fmt.Errorf("unexpected buffer cap %d, want at least %d with window %d", cap(e.hist), e.maxMatchOff+maxCompressedBlockSize, e.maxMatchOff))
			}
			// Move down. Blocks are at most maxCompressedBlockSize, so the
			// history always reaches past the window here; if it ever does
			// not, let append grow the buffer instead.
			if offset := int32(len(e.hist)) - e.maxMatchOff; offset > 0 {
				copy(e.hist[0:e.maxMatchOff], e.hist[offset:])
				e.cur += offset
				e.hist = e.hist[:e.maxMatchOff]
			}
		}
	}
	s := int32(len(e.hist))
	e.hist = append(e.hist, src...)
	return s
}

// ensureHist will ensure that history can keep at least this many bytes.
func (e *fastBase) ensureHist(n int) {
	if cap(e.hist) >= n {
		return
	}
	l := e.maxMatchOff
	if (e.lowMem && e.maxMatchOff > maxCompressedBlockSize) || e.maxMatchOff <= maxCompressedBlockSize {
		l += maxCompressedBlockSize
	} else {
		l += e.maxMatchOff
	}
	// Make it at least 1MB.
	if l < 1<<20 && !e.lowMem {
		l = 1 << 20
	}
	// Make it at least the requested size.
	if l < int32(n) {
		l = int32(n)
	}
	e.hist = make([]byte, 0, l)
}

// useBlock will replace the block with the provided one,
// but transfer recent offsets from the previous.
func (e *fastBase) UseBlock(enc *blockEnc) {
	enc.reset(e.blk)
	e.blk = enc
}

func (e *fastBase) matchlen(s, t int32, src []byte) int32 {
	if debugAsserts {
		if s < 0 {
			err := fmt.Sprintf("s (%d) < 0", s)
			panic(err)
		}
		if t < 0 {
			err := fmt.Sprintf("t (%d) < 0", t)
			panic(err)
		}
		if s-t > e.maxMatchOff {
			err := fmt.Sprintf("s (%d) - t (%d) > maxMatchOff (%d)", s, t, e.maxMatchOff)
			panic(err)
		}
		if len(src)-int(s) > maxCompressedBlockSize {
			panic(fmt.Sprintf("len(src)-s (%d) > maxCompressedBlockSize (%d)", len(src)-int(s), maxCompressedBlockSize))
		}
	}
	return int32(matchLen(src[s:], src[t:]))
}

// resetBasePrefix resets the encoder state and uses prefix as initial history,
// in place: the blocks to encode must follow it within cap(prefix).
// It reports whether positions restarted, so the caller must clear its tables.
// This is used for parallel job encoding where non-first jobs need overlap context.
// Rep offsets are set to defaults [1,4,8] (invalidated, matching C behavior).
func (e *fastBase) resetBasePrefix(prefix []byte) (restart bool) {
	if e.blk == nil {
		e.blk = &blockEnc{lowMem: e.lowMem}
		e.blk.init()
	} else {
		e.blk.reset(nil)
	}
	e.blk.initNewEncode()
	if e.crc == nil {
		e.crc = xxhash.New()
	} else {
		e.crc.Reset()
	}
	e.blk.dictLitEnc = nil
	e.inPlace(nil)
	if e.cur < e.bufferReset {
		// Bump cur so old table entries fall outside the window.
		e.cur += e.maxMatchOff + int32(len(e.hist))
	} else {
		// A shift would keep the entries just before prefix,
		// since prefix is shorter than the window.
		e.cur, restart = e.maxMatchOff, true
	}
	e.hist = e.hist[:0]
	e.inPlace(prefix)
	return restart
}

// Reset the encoding table.
func (e *fastBase) resetBase(d *dict, singleBlock bool) {
	e.inPlace(nil)
	if e.blk == nil {
		e.blk = &blockEnc{lowMem: e.lowMem}
		e.blk.init()
	} else {
		e.blk.reset(nil)
	}
	e.blk.initNewEncode()
	if e.crc == nil {
		e.crc = xxhash.New()
	} else {
		e.crc.Reset()
	}
	e.blk.dictLitEnc = nil
	if d != nil {
		low := e.lowMem
		if singleBlock {
			e.lowMem = true
		}
		e.ensureHist(d.ContentSize() + maxCompressedBlockSize)
		e.lowMem = low
	}

	// We offset current position so everything will be out of reach.
	// If above reset line, history will be purged.
	if e.cur < e.bufferReset {
		e.cur += e.maxMatchOff + int32(len(e.hist))
	}
	e.hist = e.hist[:0]
	if d != nil {
		// Set offsets (currently not used)
		for i, off := range d.offsets {
			e.blk.recentOffsets[i] = uint32(off)
			e.blk.prevRecentOffsets[i] = e.blk.recentOffsets[i]
		}
		// Transfer litenc.
		e.blk.dictLitEnc = d.litEnc
		e.blk.coders.seedPrevFromDict(d)
		e.hist = append(e.hist, d.content...)
	}
}
