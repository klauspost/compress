//go:build arm && arm.7 && !noasm && !nounsafe && gc

package zstd

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestArmAsmMatchesGo differentially tests the arm32 assembly sequence decoder
// against the pure Go reference in seqdec_arm.go. It covers incompressible,
// highly repetitive and mixed text-like inputs at every encoder level, which
// exercises literal runs and matches of all lengths, short offsets, history
// copies and repeated-offset codes.
func TestArmAsmMatchesGo(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	randBytes := make([]byte, 40000)
	r.Read(randBytes)

	rep := make([]byte, 60000)
	for i := range rep {
		rep[i] = byte((i / 17) * 3)
	}

	var mixed bytes.Buffer
	for mixed.Len() < 80000 {
		switch r.Intn(4) {
		case 0:
			mixed.WriteString("the quick brown fox jumps over the lazy dog ")
		case 1:
			b := make([]byte, r.Intn(200)+1)
			r.Read(b)
			mixed.Write(b)
		case 2:
			mixed.Write(bytes.Repeat([]byte{byte(r.Intn(4))}, r.Intn(500)+1))
		default:
			mixed.WriteString("lorem ipsum dolor sit amet consectetur adipiscing elit ")
		}
	}

	inputs := [][]byte{randBytes, rep, mixed.Bytes()}
	levels := []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression}
	sizes := []int{300, 4000, 30000, 100000}

	var asmCalls uint64
	for _, level := range levels {
		for _, n := range sizes {
			for _, in := range inputs {
				src := in
				if n < len(src) {
					src = src[:n]
				}
				var buf bytes.Buffer
				enc, err := NewWriter(&buf, WithEncoderLevel(level), WithEncoderCRC(true), WithEncoderConcurrency(1))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := enc.Write(src); err != nil {
					t.Fatal(err)
				}
				if err := enc.Close(); err != nil {
					t.Fatal(err)
				}
				comp := buf.Bytes()

				useGoArmRef = true
				want, err := decodeAll(comp)
				if err != nil {
					t.Fatalf("go ref: level=%v n=%d: %v", level, n, err)
				}
				useGoArmRef = false
				before := armFastPathCalls
				got, err := decodeAll(comp)
				asmCalls += armFastPathCalls - before
				if err != nil {
					t.Fatalf("asm: level=%v n=%d: %v", level, n, err)
				}
				if !bytes.Equal(want, got) {
					i := 0
					for i < len(want) && i < len(got) && want[i] == got[i] {
						i++
					}
					t.Fatalf("mismatch level=%v n=%d len(want)=%d len(got)=%d at byte %d", level, n, len(want), len(got), i)
				}
			}
		}
	}
	useGoArmRef = false
	if asmCalls == 0 {
		t.Fatal("the assembly decoder never ran; the test is only exercising the generic loop")
	}
}

func decodeAll(comp []byte) ([]byte, error) {
	d, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderLowmem(true))
	if err != nil {
		return nil, err
	}
	defer d.Close()
	// Give the decoder at least a block of room so small frames still select
	// the assembly path instead of falling back on capacity.
	return d.DecodeAll(comp, make([]byte, 0, maxCompressedBlockSize))
}
