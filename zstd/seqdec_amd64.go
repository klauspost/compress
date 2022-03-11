//go:build amd64 && !appengine && !noasm && gc
// +build amd64,!appengine,!noasm,gc

package zstd

import (
	"fmt"

	"github.com/klauspost/compress/internal/cpuinfo"
)

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

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm.
//
// Please refer to seqdec_generic.go for the reference implementation.
func sequenceDecs_decode_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

// sequenceDecs_decode implements the main loop of sequenceDecs in x86 asm with BMI2 extensions.
func sequenceDecs_decode_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

type sequenceDecs_decode_function = func(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int

var sequenceDecs_decode sequenceDecs_decode_function

func init() {
	if cpuinfo.HasBMI2() {
		sequenceDecs_decode = sequenceDecs_decode_bmi2
	} else {
		sequenceDecs_decode = sequenceDecs_decode_amd64
	}
}

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

	errCode := sequenceDecs_decode(s, br, &ctx)
	if errCode != 0 {
		i := len(seqs) - ctx.iteration
		switch errCode {
		case errorMatchLenOfsMismatch:
			ml := ctx.seqs[i].ml
			return fmt.Errorf("zero matchoff and matchlen (%d) > 0", ml)

		case errorMatchLenTooBig:
			ml := ctx.seqs[i].ml
			return fmt.Errorf("match len (%d) bigger than max allowed length", ml)
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

type executeAsmContext struct {
	seqs        []seqVals
	seqIndex    int
	out         []byte
	literals    []byte
	outPosition int
	litPosition int
}

// sequenceDecs_executeSimple_amd64 implements the main loop of sequenceDecs.executeSimple in x86 asm.
//
// Please refer to seqdec_generic.go for the reference implementation.
//go:noescape
func sequenceDecs_executeSimple_amd64(ctx *executeAsmContext) bool

// executeSimple handles cases when no history nor dictionary are used.
func (s *sequenceDecs) executeSimple(seqs []seqVals) error {
	// Ensure we have enough output size...
	if len(s.out)+s.seqSize > cap(s.out) {
		addBytes := s.seqSize + len(s.out)
		s.out = append(s.out, make([]byte, addBytes)...)
		s.out = s.out[:len(s.out)-addBytes]
	}

	if debugDecoder {
		printf("Execute %d seqs with literals: %d into %d bytes\n", len(seqs), len(s.literals), s.seqSize)
	}

	var t = len(s.out)
	out := s.out[:t+s.seqSize]

	ctx := executeAsmContext{
		seqs:     seqs,
		seqIndex: 0,
		out:      out,
	}

	for ctx.seqIndex < len(seqs) {
		ctx.outPosition = t
		ctx.litPosition = 0
		ctx.literals = s.literals
		completed := sequenceDecs_executeSimple_amd64(&ctx)
		if completed {
			// The asm routine did everything
			s.literals = s.literals[ctx.litPosition:]
			t = ctx.outPosition
			break
		}

		// The asm routine cannot handle seq[ctx.index]
		seq := seqs[ctx.seqIndex]
		t = ctx.outPosition
		s.literals = s.literals[ctx.litPosition:]

		ctx.seqIndex += 1

		// Add literals
		copy(out[t:], s.literals[:seq.ll])
		t += seq.ll
		s.literals = s.literals[seq.ll:]

		// Malformed input
		if seq.mo > t || seq.mo > s.windowSize {
			return fmt.Errorf("match offset (%d) bigger than current history (%d)", seq.mo, t)
		}

		// We must be in current buffer now
		if seq.ml > 0 {
			start := t - seq.mo
			if seq.ml <= t-start {
				// No overlap
				copy(out[t:], out[start:start+seq.ml])
				t += seq.ml
			} else {
				// Overlapping copy
				// Extend destination slice and copy one byte at the time.
				src := out[start : start+seq.ml]
				dst := out[t:]
				dst = dst[:len(src)]
				t += len(src)
				// Destination is the space we just added.
				for i := range src {
					dst[i] = src[i]
				}
			}
		}
	}

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
