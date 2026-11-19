//go:build arm && arm.7 && !noasm && !nounsafe && gc

package zstd

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestArmAsmMatchesGo differentially tests the arm32 assembly sequence decoder
// against the generic decodeSync loop. It covers incompressible,
// highly repetitive and mixed text-like inputs at every encoder level, exercising
// literal runs, short offsets and repeated-offset codes. Multi-block cases check
// matches wholly in history and matches spanning history and current output.
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
	armSeqdecTestHook = func(*sequenceDecs, []byte) { asmCalls++ }
	defer func() {
		armSeqdecTestHook = nil
		decodeSyncGoOnly = false
	}()
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

				before := asmCalls
				decodeSyncGoOnly = true
				want, err := decodeAll(comp)
				decodeSyncGoOnly = false
				if err != nil {
					t.Fatalf("generic: level=%v n=%d: %v", level, n, err)
				}
				if asmCalls != before || !bytes.Equal(want, src) {
					t.Fatalf("generic decoder used assembly or returned incorrect output: level=%v n=%d", level, n)
				}
				got, err := decodeAll(comp)
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
	if asmCalls == 0 {
		t.Fatal("the assembly decoder never ran; the test is only exercising the generic loop")
	}
	t.Run("multi-block-history", testArmMultiBlockHistory)
}

func testArmMultiBlockHistory(t *testing.T) {
	for _, level := range []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression} {
		t.Run(level.String(), func(t *testing.T) {
			for _, tc := range []struct {
				name                 string
				historySize, repeats int
				spanning             bool
			}{
				{"history-only", maxCompressedBlockSize, 1, false},
				{"history-and-output", maxCompressedBlockSize / 4, 4, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					history := make([]byte, tc.historySize)
					rand.New(rand.NewSource(2)).Read(history)
					tail := bytes.Repeat(history, tc.repeats)
					src := append(bytes.Clone(history), tail...)
					var buf bytes.Buffer
					enc, err := NewWriter(&buf, WithEncoderLevel(level), WithEncoderConcurrency(1), WithEncoderCRC(true))
					if err != nil {
						t.Fatal(err)
					}
					if _, err := enc.Write(history); err != nil {
						t.Fatal(err)
					}
					if err := enc.Flush(); err != nil {
						t.Fatal(err)
					}
					if _, err := enc.Write(tail); err != nil {
						t.Fatal(err)
					}
					if err := enc.Close(); err != nil {
						t.Fatal(err)
					}

					var calls, historyOnly, spanning int
					oldHook := armSeqdecTestHook
					armSeqdecTestHook = func(s *sequenceDecs, hist []byte) {
						calls++
						if len(hist) == 0 {
							return
						}
						// Inspect a private reader/state copy to prove which history branches
						// the selected assembly must take, without changing its input.
						ref, br := *s, *s.br
						ref.br = &br
						seqs := make([]seqVals, s.nSeqs)
						if err := ref.decode(seqs); err != nil {
							t.Fatal(err)
						}
						pos := len(s.out)
						for _, seq := range seqs {
							pos += seq.ll
							if back := seq.mo - pos; back > 0 {
								if back > len(hist) {
									t.Fatal("match exceeds available history")
								}
								if seq.ml <= back {
									historyOnly++
								} else if back >= 8 {
									spanning++
								}
							}
							pos += seq.ml
						}
					}
					defer func() { armSeqdecTestHook = oldHook; decodeSyncGoOnly = false }()
					decode := func() []byte {
						d, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderLowmem(true))
						if err != nil {
							t.Fatal(err)
						}
						defer d.Close()
						// A nonempty prefix keeps prior blocks in the separate history
						// buffer rather than making them part of the current block's output.
						prefix := []byte("prefix")
						dst := make([]byte, len(prefix), len(prefix)+len(src)+maxCompressedBlockSize)
						copy(dst, prefix)
						out, err := d.DecodeAll(buf.Bytes(), dst)
						if err != nil {
							t.Fatal(err)
						}
						if len(out) < len(prefix) || !bytes.Equal(out[:len(prefix)], prefix) {
							t.Fatal("prefix changed")
						}
						return out[len(prefix):]
					}
					decodeSyncGoOnly = true
					want := decode()
					if calls != 0 || !bytes.Equal(want, src) {
						t.Fatal("generic decoder used assembly or returned incorrect output")
					}
					decodeSyncGoOnly = false
					got := decode()
					if calls == 0 || !bytes.Equal(got, want) {
						t.Fatal("assembly did not run or returned incorrect output")
					}
					if (!tc.spanning && historyOnly == 0) || (tc.spanning && spanning == 0) {
						t.Fatalf("required history branch not exercised: history-only=%d spanning=%d", historyOnly, spanning)
					}
				})
			}
		})
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
