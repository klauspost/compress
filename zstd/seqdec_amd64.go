//go:build amd64 && !appengine && !noasm && gc
// +build amd64,!appengine,!noasm,gc

package zstd

import (
	"fmt"

	"github.com/klauspost/compress/internal/cpuinfo"
)

type decodeSyncAsmContext struct {
	llTable     []decSymbol
	mlTable     []decSymbol
	ofTable     []decSymbol
	llState     uint64
	mlState     uint64
	ofState     uint64
	iteration   int
	litRemain   int
	out         []byte
	outPosition int
	literals    []byte
	litPosition int
	history     []byte
	windowSize  int
	retry       bool // set by the caller when `out` got resized after reporting errorOutOfCapacity
	ll          int  // set on error (not for all errors, please refer to _generate/gen.go)
	ml          int  // set on error (not for all errors, please refer to _generate/gen.go)
	mo          int  // set on error (not for all errors, please refer to _generate/gen.go)
}

// sequenceDecs_decodeSync_amd64 implements the main loop of sequenceDecs.decodeSync in x86 asm.
//
// Please refer to seqdec_generic.go for the reference implementation.
//go:noescape
func sequenceDecs_decodeSync_amd64(s *sequenceDecs, br *bitReader, ctx *decodeSyncAsmContext) int

// sequenceDecs_decodeSync_bmi2 implements the main loop of sequenceDecs.decodeSync in x86 asm with BMI2 extensions.
//go:noescape
func sequenceDecs_decodeSync_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeSyncAsmContext) int

// decode sequences from the stream with the provided history but without a dictionary.
func (s *sequenceDecs) decodeSyncSimple(hist []byte) (bool, error) {
	if len(s.dict) > 0 {
		return false, nil
	}

	br := s.br

	maxBlockSize := maxCompressedBlockSize
	if s.windowSize < maxBlockSize {
		maxBlockSize = s.windowSize
	}

	ctx := decodeSyncAsmContext{
		llTable:     s.litLengths.fse.dt[:maxTablesize],
		mlTable:     s.matchLengths.fse.dt[:maxTablesize],
		ofTable:     s.offsets.fse.dt[:maxTablesize],
		llState:     uint64(s.litLengths.state.state),
		mlState:     uint64(s.matchLengths.state.state),
		ofState:     uint64(s.offsets.state.state),
		iteration:   s.nSeqs - 1,
		litRemain:   len(s.literals),
		out:         s.out,
		outPosition: len(s.out),
		literals:    s.literals,
		litPosition: 0,
		windowSize:  s.windowSize,
	}

	s.seqSize = 0

	for {
		var errCode int
		if cpuinfo.HasBMI2() {
			errCode = sequenceDecs_decodeSync_bmi2(s, br, &ctx)
		} else {
			errCode = sequenceDecs_decodeSync_amd64(s, br, &ctx)
		}
		if errCode == 0 {
			break
		}

		switch errCode {
		case errorOutOfCapacity:
			// Not enough size, which can happen under high volume block streaming conditions
			// but could be if destination slice is too small for sync operations.
			// over-allocating here can create a large amount of GC pressure so we try to keep
			// it as contained as possible
			used := ctx.outPosition
			addBytes := 256 + ctx.ll + ctx.ml + used>>2
			// Clamp to max block size.
			if used+addBytes > maxBlockSize {
				addBytes = maxBlockSize - used
			}
			s.out = append(s.out, make([]byte, addBytes)...)
			s.out = s.out[:len(s.out)-addBytes]

			ctx.out = s.out
			ctx.retry = true

		case errorMatchLenOfsMismatch:
			return true, fmt.Errorf("zero matchoff and matchlen (%d) > 0", ctx.ml)

		case errorMatchLenTooBig:
			return true, fmt.Errorf("match len (%d) bigger than max allowed length", ctx.ml)

		case errorMatchOffTooBig:
			return true, fmt.Errorf("match offset (%d) bigger than max allowed length (%d)", ctx.mo, ctx.outPosition)
		case errorNotEnoughLiterals:
			return true, fmt.Errorf("unexpected literal count, want %d bytes, but only %d is available", ctx.ll, ctx.litRemain+ctx.ll)
		default:
			return true, fmt.Errorf("sequenceDecs_decode_amd64 returned erronous code %d", errCode)
		}

	}

	if ctx.litRemain < 0 {
		return true, fmt.Errorf("literal count is too big: total available %d, total requested %d",
			len(s.literals), len(s.literals)-ctx.litRemain)
	}

	s.seqSize += ctx.litRemain
	if s.seqSize > maxBlockSize {
		return true, fmt.Errorf("output (%d) bigger than max block size (%d)", s.seqSize, maxBlockSize)
	}
	err := br.close()
	if err != nil {
		printf("Closing sequences: %v, %+v\n", err, *br)
		return true, err
	}

	s.literals = s.literals[ctx.litPosition:]
	t := ctx.outPosition
	s.out = s.out[:t]

	// Add final literals
	s.out = append(s.out, s.literals...)
	if debugDecoder {
		t += len(s.literals)
		if t != len(s.out) {
			panic(fmt.Errorf("length mismatch, want %d, got %d", len(s.out), t))
		}
	}

	return true, nil
}

// --------------------------------------------------------------------------------

type decodeAsmContext struct {
	llTable   []decSymbol
	mlTable   []decSymbol
	ofTable   []decSymbol
	llState   uint64
	mlState   uint64
	ofState   uint64
	iteration int
	seqs      []seqVals
	litRemain int
}

// error reported when mo == 0 && ml > 0
const errorMatchLenOfsMismatch = 1

// error reported when ml > maxMatchLen
const errorMatchLenTooBig = 2

// error reported when mo > t or mo > s.windowSize
const errorMatchOffTooBig = 3

// error reported by decodeSync when out buffer is too small
const errorOutOfCapacity = 4

// error reported when the sum of literal lengths exeeceds the literal buffer size
const errorNotEnoughLiterals = 5

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm.
//
// Please refer to seqdec_generic.go for the reference implementation.
//go:noescape
func sequenceDecs_decode_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm.
//
// Please refer to seqdec_generic.go for the reference implementation.
//go:noescape
func sequenceDecs_decode_56_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm with BMI2 extensions.
//go:noescape
func sequenceDecs_decode_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm with BMI2 extensions.
//go:noescape
func sequenceDecs_decode_56_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

// decode sequences from the stream without the provided history.
func (s *sequenceDecs) decode(seqs []seqVals) error {
	br := s.br

	maxBlockSize := maxCompressedBlockSize
	if s.windowSize < maxBlockSize {
		maxBlockSize = s.windowSize
	}

	ctx := decodeAsmContext{
		llTable:   s.litLengths.fse.dt[:maxTablesize],
		mlTable:   s.matchLengths.fse.dt[:maxTablesize],
		ofTable:   s.offsets.fse.dt[:maxTablesize],
		llState:   uint64(s.litLengths.state.state),
		mlState:   uint64(s.matchLengths.state.state),
		ofState:   uint64(s.offsets.state.state),
		seqs:      seqs,
		iteration: len(seqs) - 1,
		litRemain: len(s.literals),
	}

	s.seqSize = 0
	lte56bits := s.maxBits+s.offsets.fse.actualTableLog+s.matchLengths.fse.actualTableLog+s.litLengths.fse.actualTableLog <= 56
	var errCode int
	if cpuinfo.HasBMI2() {
		if lte56bits {
			errCode = sequenceDecs_decode_56_bmi2(s, br, &ctx)
		} else {
			errCode = sequenceDecs_decode_bmi2(s, br, &ctx)
		}
	} else {
		if lte56bits {
			errCode = sequenceDecs_decode_56_amd64(s, br, &ctx)
		} else {
			errCode = sequenceDecs_decode_amd64(s, br, &ctx)
		}
	}
	if errCode != 0 {
		i := len(seqs) - ctx.iteration
		switch errCode {
		case errorMatchLenOfsMismatch:
			ml := ctx.seqs[i].ml
			return fmt.Errorf("zero matchoff and matchlen (%d) > 0", ml)

		case errorMatchLenTooBig:
			ml := ctx.seqs[i].ml
			return fmt.Errorf("match len (%d) bigger than max allowed length", ml)

		case errorNotEnoughLiterals:
			ll := ctx.seqs[i].ll
			fmt.Errorf("unexpected literal count, want %d bytes, but only %d is available", ll, ctx.litRemain+ll)
		}

		return fmt.Errorf("sequenceDecs_decode_amd64 returned erronous code %d", errCode)
	}

	if ctx.litRemain < 0 {
		return fmt.Errorf("literal count is too big: total available %d, total requested %d",
			len(s.literals), len(s.literals)-ctx.litRemain)
	}

	s.seqSize += ctx.litRemain
	if s.seqSize > maxBlockSize {
		return fmt.Errorf("output (%d) bigger than max block size (%d)", s.seqSize, maxBlockSize)
	}
	err := br.close()
	if err != nil {
		printf("Closing sequences: %v, %+v\n", err, *br)
	}
	return err
}

// --------------------------------------------------------------------------------

type executeAsmContext struct {
	seqs        []seqVals
	seqIndex    int
	out         []byte
	history     []byte
	literals    []byte
	outPosition int
	litPosition int
	windowSize  int
}

// sequenceDecs_executeSimple_amd64 implements the main loop of sequenceDecs.executeSimple in x86 asm.
//
// Returns false if a match offset is too big.
//
// Please refer to seqdec_generic.go for the reference implementation.
//go:noescape
func sequenceDecs_executeSimple_amd64(ctx *executeAsmContext) bool

// executeSimple handles cases when dictionary is not used.
func (s *sequenceDecs) executeSimple(seqs []seqVals, hist []byte) error {
	// Ensure we have enough output size...
	if len(s.out)+s.seqSize+compressedBlockOverAlloc > cap(s.out) {
		addBytes := s.seqSize + len(s.out) + compressedBlockOverAlloc
		s.out = append(s.out, make([]byte, addBytes)...)
		s.out = s.out[:len(s.out)-addBytes]
	}

	if debugDecoder {
		printf("Execute %d seqs with literals: %d into %d bytes\n", len(seqs), len(s.literals), s.seqSize)
	}

	var t = len(s.out)
	out := s.out[:t+s.seqSize]

	ctx := executeAsmContext{
		seqs:        seqs,
		seqIndex:    0,
		out:         out,
		history:     hist,
		outPosition: t,
		litPosition: 0,
		literals:    s.literals,
		windowSize:  s.windowSize,
	}

	ok := sequenceDecs_executeSimple_amd64(&ctx)
	if !ok {
		return fmt.Errorf("match offset (%d) bigger than current history (%d)",
			seqs[ctx.seqIndex].mo, ctx.outPosition+len(hist))
	}
	s.literals = s.literals[ctx.litPosition:]
	t = ctx.outPosition

	// Add final literals
	copy(out[t:], s.literals)
	if debugDecoder {
		t += len(s.literals)
		if t != len(out) {
			panic(fmt.Errorf("length mismatch, want %d, got %d, ss: %d", len(out), t, s.seqSize))
		}
	}
	s.out = out

	return nil
}
