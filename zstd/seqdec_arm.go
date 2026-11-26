//go:build arm && arm.7 && !noasm && !nounsafe && gc

package zstd

import (
	"fmt"
	"io"
	"unsafe"
)

// Error codes exchanged with the ARM assembly.
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

// armSeqCtx is the flat 4-byte-aligned descriptor handed to the assembly. All
// fields are 32 bits wide so field offsets stay uniform on 32-bit targets;
// seqdec_arm.s accesses them through go_asm.h.
type armSeqCtx struct {
	inBase   unsafe.Pointer
	cursor   int32
	bitsRead int32
	valHi    uint32
	valLo    uint32

	llTable unsafe.Pointer
	mlTable unsafe.Pointer
	ofTable unsafe.Pointer

	llHdr  uint32
	llBase uint32
	mlHdr  uint32
	mlBase uint32
	ofHdr  uint32
	ofBase uint32

	prev0 int32
	prev1 int32
	prev2 int32

	nSeqs int32

	outBase unsafe.Pointer
	outLen  int32
	outCap  int32

	litBase unsafe.Pointer
	litLen  int32
	litPos  int32

	histBase unsafe.Pointer
	histLen  int32

	windowSize int32
	maxBlock   int32
	startSize  int32
	maxBits    int32

	outPos int32 // result

	errLL int32
	errML int32
	errMO int32
}

// armSeqdecTestHook is nil outside path-selection tests.
var armSeqdecTestHook func(*sequenceDecs, []byte)

//go:noescape
func sequenceDecs_decodeSync_arm(ctx *armSeqCtx) int32

// decodeSyncSimple implements the arm32 fast path with the assembly decoder.
func (s *sequenceDecs) decodeSyncSimple(hist []byte) (bool, error) {
	if len(s.dict) > 0 {
		return false, nil
	}
	maxBlockSize := min(s.windowSize, maxCompressedBlockSize)
	if s.maxSyncLen == 0 && cap(s.out)-len(s.out) < maxBlockSize {
		return false, nil
	}

	if armSeqdecTestHook != nil {
		armSeqdecTestHook(s, hist)
	}
	br := s.br
	llD := s.litLengths.state.state
	mlD := s.matchLengths.state.state
	ofD := s.offsets.state.state
	ctx := armSeqCtx{
		inBase:     unsafe.Pointer(unsafe.SliceData(br.in)),
		cursor:     int32(br.cursor),
		bitsRead:   int32(br.bitsRead),
		valHi:      uint32(br.value >> 32),
		valLo:      uint32(br.value),
		llTable:    unsafe.Pointer(unsafe.SliceData(s.litLengths.fse.dt[:])),
		mlTable:    unsafe.Pointer(unsafe.SliceData(s.matchLengths.fse.dt[:])),
		ofTable:    unsafe.Pointer(unsafe.SliceData(s.offsets.fse.dt[:])),
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
		outBase:    unsafe.Pointer(unsafe.SliceData(s.out)),
		outLen:     int32(len(s.out)),
		outCap:     int32(s.syncOutCap()),
		litBase:    unsafe.Pointer(unsafe.SliceData(s.literals)),
		litLen:     int32(len(s.literals)),
		litPos:     0,
		histBase:   unsafe.Pointer(unsafe.SliceData(hist)),
		histLen:    int32(len(hist)),
		windowSize: int32(s.windowSize),
		maxBlock:   int32(maxBlockSize),
		startSize:  int32(len(s.out)),
		maxBits:    int32(s.maxBits),
	}
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
	case errorNotEnoughSpace, errorNotEnoughCapacity:
		if errCode == errorNotEnoughCapacity && s.limitToCap {
			return true, ErrDecoderSizeExceeded
		}
		return true, fmt.Errorf("output bigger than max block size (%d)", maxBlockSize)
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
	if s.limitToCap && s.seqSize > s.outputLimit {
		return true, ErrDecoderSizeExceeded
	}
	s.literals = s.literals[ctx.litPos:]
	s.out = s.out[:ctx.outPos]
	s.out = append(s.out, s.literals...)
	return true, nil
}
