//go:build arm && arm.7 && !noasm && !nounsafe && gc

package zstd

import (
	"errors"
	"fmt"
	"io"
	"unsafe"
)

// The generic sequence decoder is slow on 32-bit ARM for two reasons that do
// not apply to any 64-bit target:
//
//   - bitReader.value is a uint64 and every extraction shifts it by a
//     variable amount, which the compiler turns into a multi-instruction
//     double-word shift sequence (arm has no 64-bit shift).
//   - every literal run and match is copied with append/copy, i.e. a call to
//     runtime.memmove. zstd sequences are short: on the tile corpus 58% of
//     sequences have no literals at all, 99% at most 16, and the median match
//     is under 16 bytes, so the call overhead dominates the copy itself.
//
// This file keeps the same bit reader state split into two 32-bit words and
// copies with explicit 4-byte moves, avoiding both.
//
// armBitReader mirrors zstd's reverse bit reader. `value` is the 64-bit word
// le.Load64 would produce, held as hi:lo.
type armBitReader struct {
	in       unsafe.Pointer
	cursor   int
	bitsRead int
	hi, lo   uint32
}

// init resets b to read the bit stream in in, which must end with a non-zero
// byte whose highest set bit marks the start of the stream.
func (b *armBitReader) init(in []byte) error {
	if len(in) < 1 {
		return errors.New("corrupt stream: too short")
	}
	v := in[len(in)-1]
	if v == 0 {
		return errors.New("corrupt stream, did not find end of stream")
	}
	b.in = unsafe.Pointer(unsafe.SliceData(in))
	b.cursor = len(in)
	b.bitsRead = 64
	b.hi, b.lo = 0, 0
	if len(in) >= 8 {
		b.fillFastStart()
	} else {
		b.fill()
		b.fill()
	}
	b.bitsRead += 8 - int(highBits(uint32(v)))
	return nil
}

// fillFastStart loads the first 64 bits. It requires at least 8 bytes.
func (b *armBitReader) fillFastStart() {
	b.cursor -= 8
	p := unsafe.Add(b.in, b.cursor)
	b.lo = *(*uint32)(p)
	b.hi = *(*uint32)(unsafe.Add(p, 4))
	b.bitsRead = 0
}

// fillFast shifts in 32 bits when fewer than 32 are left. It requires at
// least 4 unread bytes.
func (b *armBitReader) fillFast() {
	if b.bitsRead < 32 {
		return
	}
	b.cursor -= 4
	b.hi = b.lo
	b.lo = *(*uint32)(unsafe.Add(b.in, b.cursor))
	b.bitsRead -= 32
}

// fill shifts in 32 bits when fewer than 32 are left, falling back to a byte
// at a time for the last few bytes of the stream.
func (b *armBitReader) fill() {
	if b.bitsRead < 32 {
		return
	}
	if b.cursor >= 4 {
		b.cursor -= 4
		b.hi = b.lo
		b.lo = *(*uint32)(unsafe.Add(b.in, b.cursor))
		b.bitsRead -= 32
		return
	}
	b.bitsRead -= 8 * b.cursor
	for b.cursor > 0 {
		b.cursor--
		c := uint32(*(*uint8)(unsafe.Add(b.in, b.cursor)))
		b.hi = b.hi<<8 | b.lo>>24
		b.lo = b.lo<<8 | c
	}
}

// getBits returns n bits, n in [0,32]. The top n bits of value<<bitsRead.
// getBits returns n bits from the top of the window, n in [0,32], and advances
// the reader by n.
func (b *armBitReader) getBits(n int) uint32 {
	if n == 0 {
		return 0
	}
	s := b.bitsRead
	var h uint32
	if s < 32 {
		// Go defines shifts >= width as zero, so s == 0 needs no branch.
		h = b.hi<<uint(s) | b.lo>>uint(32-s)
	} else {
		h = b.lo << uint(s-32)
	}
	b.bitsRead = s + n
	return h >> uint(32-n)
}

// close releases the input and reports whether the stream was consumed
// exactly. It must be called once decoding is finished.
func (b *armBitReader) close() error {
	b.in = nil
	b.cursor = 0
	if b.cursor != 0 || b.bitsRead < 64 {
		return fmt.Errorf("%d extra bits on block, should be 0", 8*uint(b.cursor)+64-uint(b.bitsRead))
	}
	if b.bitsRead > 64 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// armSymbol is the in-memory layout of decSymbol: one packed 32-bit word
// holding nbBits, addBits and newState, followed by the 32-bit baseline.
type armSymbol struct {
	hdr  uint32 // nbBits | addBits<<8 | newState<<16
	base uint32
}

func loadArmSymbol(tab unsafe.Pointer, idx uint32) armSymbol {
	return *(*armSymbol)(unsafe.Add(tab, idx*8))
}

const armTableMask = maxTableMask // 511

// Error codes shared with the amd64 assembly path, which defines them in its
// own build-tagged file.
const (
	noError                  = 0
	errorMatchLenOfsMismatch = 1
	errorMatchLenTooBig      = 2
	errorMatchOffTooBig      = 3
	errorNotEnoughLiterals   = 4
	errorNotEnoughSpace      = 5
	errorOverread            = 6
	errorNotEnoughCapacity   = 7
)

func (s *sequenceDecs) decodeSyncSimpleGo(hist []byte) (bool, error) {
	if len(s.dict) > 0 {
		return false, nil
	}
	maxBlockSize := maxCompressedBlockSize
	if s.windowSize < maxBlockSize {
		maxBlockSize = s.windowSize
	}
	// The fast path never grows s.out. When the frame size is known the
	// decoder may have sized the buffer to the remaining frame rather than to
	// a whole block; the copy loops then check the real capacity.
	if s.maxSyncLen == 0 && cap(s.out)-len(s.out) < maxBlockSize {
		return false, nil
	}

	br := s.br
	var ar armBitReader
	ar.in = unsafe.Pointer(unsafe.SliceData(br.in))
	ar.cursor = br.cursor
	ar.bitsRead = int(br.bitsRead)
	ar.hi = uint32(br.value >> 32)
	ar.lo = uint32(br.value)

	llTab := unsafe.Pointer(unsafe.SliceData(s.litLengths.fse.dt[:]))
	mlTab := unsafe.Pointer(unsafe.SliceData(s.matchLengths.fse.dt[:]))
	ofTab := unsafe.Pointer(unsafe.SliceData(s.offsets.fse.dt[:]))
	llD := s.litLengths.state.state
	mlD := s.matchLengths.state.state
	ofD := s.offsets.state.state
	llState := armSymbol{hdr: uint32(llD), base: uint32(llD >> 32)}
	mlState := armSymbol{hdr: uint32(mlD), base: uint32(mlD >> 32)}
	ofState := armSymbol{hdr: uint32(ofD), base: uint32(ofD >> 32)}
	prev0, prev1, prev2 := s.prevOffset[0], s.prevOffset[1], s.prevOffset[2]

	outBase := unsafe.Pointer(unsafe.SliceData(s.out))
	outPos := len(s.out)
	startSize := outPos
	outEnd := cap(s.out)

	litBase := unsafe.Pointer(unsafe.SliceData(s.literals))
	litPos := 0
	litLen := len(s.literals)

	histBase := unsafe.Pointer(unsafe.SliceData(hist))
	histLen := len(hist)
	windowSize := s.windowSize
	maxBits := s.maxBits

	fail := func(code int, ll, ml, mo int) (bool, error) {
		// Publish the reader state so error paths and callers see progress.
		br.cursor = ar.cursor
		br.bitsRead = uint8(ar.bitsRead)
		br.value = uint64(ar.hi)<<32 | uint64(ar.lo)
		switch code {
		case errorMatchLenOfsMismatch:
			return true, fmt.Errorf("zero matchoff and matchlen (%d) > 0", ml)
		case errorMatchLenTooBig:
			return true, fmt.Errorf("match len (%d) bigger than max allowed length", ml)
		case errorMatchOffTooBig:
			return true, fmt.Errorf("match offset (%d) bigger than current history (%d)", mo, outPos+histLen-startSize)
		case errorNotEnoughLiterals:
			return true, fmt.Errorf("unexpected literal count, want %d bytes, but only %d is available", ll, litLen-litPos+ll)
		case errorNotEnoughCapacity:
			return true, ErrDecoderSizeExceeded
		default:
			return true, fmt.Errorf("sequenceDecs_decode returned erroneous code %d", code)
		}
	}

	for i := s.nSeqs - 1; i >= 0; i-- {
		if ar.bitsRead > 64 {
			return fail(errorOverread, 0, 0, 0)
		}
		var ll, mo, ml int
		if ar.cursor > 11 {
			llB := int((llState.hdr >> 8) & 0xff)
			mlB := int((mlState.hdr >> 8) & 0xff)
			moB := int((ofState.hdr >> 8) & 0xff)
			ll = int(llState.base)
			ml = int(mlState.base)
			mo = int(ofState.base)
			ar.fillFast()
			mo += int(ar.getBits(moB))
			if maxBits > 32 {
				ar.fillFast()
			}
			ml += int(ar.getBits(mlB))
			ll += int(ar.getBits(llB))
			if moB > 1 {
				prev2, prev1, prev0 = prev1, prev0, mo
			} else {
				if ll == 0 {
					mo++
				}
				if mo == 0 {
					mo = prev0
				} else {
					var temp int
					if mo == 3 {
						temp = prev0 - 1
					} else {
						temp = [3]int{prev0, prev1, prev2}[mo]
					}
					if temp == 0 {
						temp = 1
					}
					if mo != 1 {
						prev2 = prev1
					}
					prev1 = prev0
					prev0 = temp
					mo = temp
				}
			}
			ar.fillFast()
		} else {
			llB := int((llState.hdr >> 8) & 0xff)
			mlB := int((mlState.hdr >> 8) & 0xff)
			moB := int((ofState.hdr >> 8) & 0xff)
			ll = int(llState.base)
			ml = int(mlState.base)
			mo = int(ofState.base)
			ar.fill()
			mo += int(ar.getBits(moB))
			if maxBits > 32 {
				ar.fill()
			}
			ml += int(ar.getBits(mlB))
			ll += int(ar.getBits(llB))
			if moB > 1 {
				prev2, prev1, prev0 = prev1, prev0, mo
			} else {
				if ll == 0 {
					mo++
				}
				if mo == 0 {
					mo = prev0
				} else {
					var temp int
					if mo == 3 {
						temp = prev0 - 1
					} else {
						temp = [3]int{prev0, prev1, prev2}[mo]
					}
					if temp == 0 {
						temp = 1
					}
					if mo != 1 {
						prev2 = prev1
					}
					prev1 = prev0
					prev0 = temp
					mo = temp
				}
			}
			ar.fill()
		}

		if ll > litLen-litPos {
			return fail(errorNotEnoughLiterals, ll, ml, mo)
		}
		if outPos+ll+ml-startSize > maxBlockSize {
			return fail(errorNotEnoughSpace, ll, ml, mo)
		}
		if outPos+ll+ml > outEnd {
			return fail(errorNotEnoughCapacity, ll, ml, mo)
		}
		if ml > maxMatchLen {
			return fail(errorMatchLenTooBig, ll, ml, mo)
		}

		dst := unsafe.Add(outBase, outPos)
		src := unsafe.Add(litBase, litPos)
		copyExact(dst, src, ll)
		outPos += ll
		litPos += ll

		if mo == 0 && ml > 0 {
			// roll back the literal copy so error reporting matches the
			// reference implementation's state
			outPos -= ll
			litPos -= ll
			return fail(errorMatchLenOfsMismatch, ll, ml, mo)
		}
		if ml > 0 {
			if mo > outPos+histLen || mo > windowSize {
				outPos -= ll
				litPos -= ll
				return fail(errorMatchOffTooBig, ll, ml, mo)
			}
			dst = unsafe.Add(outBase, outPos)
			if v := mo - outPos; v > 0 {
				start := histLen - v
				src = unsafe.Add(histBase, start)
				if ml > v {
					copyExact(dst, src, v)
					outPos += v
					ml -= v
					dst = unsafe.Add(outBase, outPos)
				} else {
					copyExact(dst, src, ml)
					outPos += ml
					ml = 0
				}
			}
			if ml > 0 {
				src = unsafe.Add(outBase, outPos-mo)
				copyMatch(dst, src, ml, mo)
				outPos += ml
			}
		}

		if i == 0 {
			break
		}

		nBits := int(llState.hdr&0xff) + int(mlState.hdr&0xff) + int(ofState.hdr&0xff)
		if nBits == 0 {
			llState = loadArmSymbol(llTab, (llState.hdr>>16)&armTableMask)
			mlState = loadArmSymbol(mlTab, (mlState.hdr>>16)&armTableMask)
			ofState = loadArmSymbol(ofTab, (ofState.hdr>>16)&armTableMask)
		} else {
			bits := ar.getBits(nBits)
			llNb := int(llState.hdr & 0xff)
			mlNb := int(mlState.hdr & 0xff)
			ofNb := int(ofState.hdr & 0xff)
			lowBits := (bits >> uint((ofNb+mlNb)&31)) & uint32(bitMask[llNb&15])
			llState = loadArmSymbol(llTab, ((llState.hdr>>16)+lowBits)&armTableMask)
			lowBits = (bits >> uint(ofNb&31)) & uint32(bitMask[mlNb&15])
			mlState = loadArmSymbol(mlTab, ((mlState.hdr>>16)+lowBits)&armTableMask)
			lowBits = bits & uint32(bitMask[ofNb&15])
			ofState = loadArmSymbol(ofTab, ((ofState.hdr>>16)+lowBits)&armTableMask)
		}
	}

	br.cursor = ar.cursor
	br.bitsRead = uint8(ar.bitsRead)
	br.value = uint64(ar.hi)<<32 | uint64(ar.lo)
	s.prevOffset[0], s.prevOffset[1], s.prevOffset[2] = prev0, prev1, prev2

	litRemain := litLen - litPos
	s.seqSize = outPos - startSize + litRemain
	if s.seqSize > maxBlockSize {
		return true, fmt.Errorf("output bigger than max block size (%d)", maxBlockSize)
	}
	if outPos > outEnd {
		return true, fmt.Errorf("output bigger than max block size (%d)", maxBlockSize)
	}
	if err := br.close(); err != nil {
		return true, err
	}
	s.literals = s.literals[litPos:]
	s.out = s.out[:outPos]
	s.out = append(s.out, s.literals...)
	return true, nil
}

// copyExact copies n bytes. n is small (literals: <=16 in 99% of sequences).
func copyExact(dst, src unsafe.Pointer, n int) {
	for n >= 16 {
		*(*uint32)(dst) = *(*uint32)(src)
		*(*uint32)(unsafe.Add(dst, 4)) = *(*uint32)(unsafe.Add(src, 4))
		*(*uint32)(unsafe.Add(dst, 8)) = *(*uint32)(unsafe.Add(src, 8))
		*(*uint32)(unsafe.Add(dst, 12)) = *(*uint32)(unsafe.Add(src, 12))
		dst = unsafe.Add(dst, 16)
		src = unsafe.Add(src, 16)
		n -= 16
	}
	if n >= 8 {
		*(*uint32)(dst) = *(*uint32)(src)
		*(*uint32)(unsafe.Add(dst, 4)) = *(*uint32)(unsafe.Add(src, 4))
		dst = unsafe.Add(dst, 8)
		src = unsafe.Add(src, 8)
		n -= 8
	}
	if n >= 4 {
		*(*uint32)(dst) = *(*uint32)(src)
		dst = unsafe.Add(dst, 4)
		src = unsafe.Add(src, 4)
		n -= 4
	}
	if n >= 2 {
		*(*uint16)(dst) = *(*uint16)(src)
		dst = unsafe.Add(dst, 2)
		src = unsafe.Add(src, 2)
		n -= 2
	}
	if n == 1 {
		*(*uint8)(dst) = *(*uint8)(src)
	}
}

// copyMatch copies an LZ77 match of length n from src (dst-offset) to dst.
func copyMatch(dst, src unsafe.Pointer, n, offset int) {
	if offset >= 8 {
		for n >= 8 {
			*(*uint32)(dst) = *(*uint32)(src)
			*(*uint32)(unsafe.Add(dst, 4)) = *(*uint32)(unsafe.Add(src, 4))
			dst = unsafe.Add(dst, 8)
			src = unsafe.Add(src, 8)
			n -= 8
		}
		copyExact(dst, src, n)
		return
	}
	// Short offsets are 1% of sequences; the byte loop keeps it simple and
	// it is still faster than the memmove call it replaces.
	for i := 0; i < n; i++ {
		*(*uint8)(unsafe.Add(dst, i)) = *(*uint8)(unsafe.Add(src, i))
	}
}

// armSeqCtx is the flat 4-byte-aligned descriptor handed to the assembly. All
// fields are 32 bits wide so asm offsets are independent of the Go struct
// layout rules for 64-bit fields on 32-bit targets.
type armSeqCtx struct {
	inBase   uintptr // 0
	cursor   int32   // 4
	bitsRead int32   // 8
	valHi    uint32  // 12
	valLo    uint32  // 16

	llTable uintptr // 20
	mlTable uintptr // 24
	ofTable uintptr // 28

	llHdr  uint32 // 32
	llBase uint32 // 36
	mlHdr  uint32 // 40
	mlBase uint32 // 44
	ofHdr  uint32 // 48
	ofBase uint32 // 52

	prev0 int32 // 56
	prev1 int32 // 60
	prev2 int32 // 64

	nSeqs int32 // 68

	outBase uintptr // 72
	outLen  int32   // 76
	outCap  int32   // 80

	litBase uintptr // 84
	litLen  int32   // 88
	litPos  int32   // 92

	histBase uintptr // 96
	histLen  int32   // 100

	windowSize int32 // 104
	maxBlock   int32 // 108
	startSize  int32 // 112
	maxBits    int32 // 116

	outPos int32 // 120 (result)

	errLL int32 // 124
	errML int32 // 128
	errMO int32 // 132
}

// useGoArmRef selects the Go reference implementation; used by tests.
var useGoArmRef = false

//go:noescape
func sequenceDecs_decodeSync_arm(ctx *armSeqCtx) int32

// decodeSyncSimple implements the arm32 fast path with the assembly decoder.
func (s *sequenceDecs) decodeSyncSimple(hist []byte) (bool, error) {
	if useGoArmRef {
		return s.decodeSyncSimpleGo(hist)
	}
	if len(s.dict) > 0 {
		return false, nil
	}
	maxBlockSize := maxCompressedBlockSize
	if s.windowSize < maxBlockSize {
		maxBlockSize = s.windowSize
	}
	// The fast path never grows s.out. When the frame size is known the
	// decoder may have sized the buffer to the remaining frame rather than to
	// a whole block; the copy loops then check the real capacity.
	if s.maxSyncLen == 0 && cap(s.out)-len(s.out) < maxBlockSize {
		return false, nil
	}

	br := s.br
	llD := s.litLengths.state.state
	mlD := s.matchLengths.state.state
	ofD := s.offsets.state.state
	ctx := armSeqCtx{
		inBase:     uintptr(unsafe.Pointer(unsafe.SliceData(br.in))),
		cursor:     int32(br.cursor),
		bitsRead:   int32(br.bitsRead),
		valHi:      uint32(br.value >> 32),
		valLo:      uint32(br.value),
		llTable:    uintptr(unsafe.Pointer(unsafe.SliceData(s.litLengths.fse.dt[:]))),
		mlTable:    uintptr(unsafe.Pointer(unsafe.SliceData(s.matchLengths.fse.dt[:]))),
		ofTable:    uintptr(unsafe.Pointer(unsafe.SliceData(s.offsets.fse.dt[:]))),
		llHdr:      uint32(llD),
		llBase:     uint32(llD >> 32),
		mlHdr:      uint32(mlD),
		mlBase:     uint32(mlD >> 32),
		ofHdr:      uint32(ofD),
		ofBase:     uint32(ofD >> 32),
		prev0:      int32(s.prevOffset[0]),
		prev1:      int32(s.prevOffset[1]),
		prev2:      int32(s.prevOffset[2]),
		nSeqs:      int32(s.nSeqs),
		outBase:    uintptr(unsafe.Pointer(unsafe.SliceData(s.out))),
		outLen:     int32(len(s.out)),
		outCap:     int32(cap(s.out)),
		litBase:    uintptr(unsafe.Pointer(unsafe.SliceData(s.literals))),
		litLen:     int32(len(s.literals)),
		litPos:     0,
		histBase:   uintptr(unsafe.Pointer(unsafe.SliceData(hist))),
		histLen:    int32(len(hist)),
		windowSize: int32(s.windowSize),
		maxBlock:   int32(maxBlockSize),
		startSize:  int32(len(s.out)),
		maxBits:    int32(s.maxBits),
	}
	armFastPathCalls++
	s.seqSize = 0
	startSize := len(s.out)

	switch errCode := sequenceDecs_decodeSync_arm(&ctx); errCode {
	case noError:
	case errorMatchLenOfsMismatch:
		return true, fmt.Errorf("zero matchoff and matchlen (%d) > 0", ctx.errML)
	case errorMatchLenTooBig:
		return true, fmt.Errorf("match len (%d) bigger than max allowed length", ctx.errML)
	case errorMatchOffTooBig:
		return true, fmt.Errorf("match offset (%d) bigger than current history (%d)",
			ctx.errMO, int(ctx.outPos)-startSize)
	case errorNotEnoughLiterals:
		return true, fmt.Errorf("unexpected literal count, want %d bytes, but only %d is available",
			ctx.errLL, int(ctx.litLen-ctx.litPos)+int(ctx.errLL))
	case errorNotEnoughSpace:
		return true, fmt.Errorf("output bigger than max block size (%d)", maxBlockSize)
	case errorNotEnoughCapacity:
		return true, ErrDecoderSizeExceeded
	case errorOverread:
		return true, io.ErrUnexpectedEOF
	default:
		return true, fmt.Errorf("sequenceDecs_decode returned erroneous code %d", errCode)
	}

	br.cursor = int(ctx.cursor)
	br.bitsRead = uint8(ctx.bitsRead)
	br.value = uint64(ctx.valHi)<<32 | uint64(ctx.valLo)
	s.prevOffset[0] = int(ctx.prev0)
	s.prevOffset[1] = int(ctx.prev1)
	s.prevOffset[2] = int(ctx.prev2)

	s.seqSize = int(ctx.outPos) - startSize + int(ctx.litLen-ctx.litPos)
	if s.seqSize > maxBlockSize {
		return true, fmt.Errorf("output bigger than max block size (%d)", maxBlockSize)
	}
	if err := br.close(); err != nil {
		return true, err
	}
	s.literals = s.literals[ctx.litPos:]
	s.out = s.out[:ctx.outPos]
	s.out = append(s.out, s.literals...)
	return true, nil
}

// armFastPathCalls counts how often the assembly decoder ran. The differential
// test uses it to prove that the cases it generated actually took the fast
// path instead of silently falling back to the generic loop.
var armFastPathCalls uint64
