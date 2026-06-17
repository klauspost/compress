// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// getConcBlockOpts generates option combinations for concurrent blocks testing.
// Similar to getEncOpts but always includes WithConcurrentBlocks(true).
func getConcBlockOpts(cMax int) []testEncOpt {
	var o []testEncOpt
	for level := speedNotSet + 1; level < speedLast; level++ {
		if isRaceTest && level >= SpeedBestCompression {
			break
		}
		for conc := 2; conc <= 4; conc *= 2 {
			for _, wind := range testWindowSizes {
				addOpt := func(name string, options ...EOption) {
					opts := append([]EOption(nil),
						WithEncoderLevel(level),
						WithEncoderConcurrency(conc),
						WithWindowSize(wind),
						WithConcurrentBlocks(true),
					)
					name = fmt.Sprintf("%s-c%d-w%dk-%s", level.String(), conc, wind/1024, name)
					o = append(o, testEncOpt{name: name, o: append(opts, options...)})
				}
				addOpt("default")
				if testing.Short() {
					break
				}
				addOpt("nocrc", WithEncoderCRC(false))
				addOpt("lowmem", WithLowerEncoderMem(true))
				addOpt("alllit", WithAllLitEntropyCompression(true))
				addOpt("nolit", WithNoEntropyCompression(true))
				addOpt("pad1k", WithEncoderPadding(1024))
				addOpt("zerof", WithZeroFrames(true))
			}
			if testing.Short() && conc == 2 {
				break
			}
			if conc >= cMax {
				break
			}
		}
	}
	return o
}

func TestConcurrentBlocks_RoundTrip(t *testing.T) {
	levels := []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression}
	sizes := []int{0, 1, 100, 1 << 16, 1 << 20, 4 << 20}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(42))

	for _, level := range levels {
		for _, size := range sizes {
			for _, conc := range []int{2, 4} {
				name := fmt.Sprintf("%s-sz%d-c%d", level, size, conc)
				t.Run(name, func(t *testing.T) {
					input := make([]byte, size)
					for i := range input {
						if rng.Intn(3) == 0 {
							input[i] = byte(rng.Intn(16))
						} else {
							input[i] = byte(rng.Intn(256))
						}
					}

					var buf bytes.Buffer
					enc, err := NewWriter(&buf,
						WithEncoderLevel(level),
						WithEncoderConcurrency(conc),
						WithConcurrentBlocks(true),
					)
					if err != nil {
						t.Fatal(err)
					}
					n, err := enc.Write(input)
					if err != nil {
						t.Fatal(err)
					}
					if n != len(input) {
						t.Fatalf("short write: %d != %d", n, len(input))
					}
					if err := enc.Close(); err != nil {
						t.Fatal(err)
					}

					decoded, err := dec.DecodeAll(buf.Bytes(), nil)
					if err != nil {
						t.Fatalf("decode error (size=%d, conc=%d): %v", size, conc, err)
					}
					if !bytes.Equal(decoded, input) {
						t.Fatalf("round-trip mismatch (size=%d, conc=%d): got %d bytes, want %d",
							size, conc, len(decoded), len(input))
					}
				})
			}
		}
	}
}

func TestConcurrentBlocks_Regression(t *testing.T) {
	defer timeout(4 * time.Minute)()
	data, err := os.ReadFile("testdata/comp-crashers.zip")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for _, opts := range getConcBlockOpts(2) {
		t.Run(opts.name, func(t *testing.T) {
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			enc, err := NewWriter(nil, opts.o...)
			if err != nil {
				t.Fatal(err)
			}
			defer enc.Close()

			for i, tt := range zr.File {
				if testing.Short() && i > 10 {
					break
				}
				t.Run(tt.Name, func(t *testing.T) {
					r, err := tt.Open()
					if err != nil {
						t.Error(err)
						return
					}
					in, err := io.ReadAll(r)
					if err != nil {
						t.Error(err)
					}

					// Test EncodeAll path (uses encoder pool, not concurrent blocks).
					encoded := enc.EncodeAll(in, nil)
					decoded, err := dec.DecodeAll(encoded, nil)
					if err != nil {
						t.Errorf("EncodeAll decode error: %v", err)
						return
					}
					if !bytes.Equal(decoded, in) {
						t.Error("EncodeAll round-trip mismatch")
						return
					}

					// Test Write path (uses concurrent blocks).
					var buf bytes.Buffer
					enc.Reset(&buf)
					_, err = enc.Write(in)
					if err != nil {
						t.Error(err)
						return
					}
					err = enc.Close()
					if err != nil {
						t.Error(err)
						return
					}
					decoded, err = dec.DecodeAll(buf.Bytes(), nil)
					if err != nil {
						t.Errorf("Write decode error: %v", err)
						return
					}
					if !bytes.Equal(decoded, in) {
						t.Error("Write round-trip mismatch")
					}
				})
			}
		})
	}
}

func TestConcurrentBlocks_CRCCorrectness(t *testing.T) {
	rng := mrand.New(mrand.NewSource(88))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for _, level := range []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression} {
		t.Run(level.String(), func(t *testing.T) {
			// Encode with concurrent blocks.
			var buf1 bytes.Buffer
			enc1, err := NewWriter(&buf1,
				WithEncoderLevel(level),
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(1<<20),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc1.Write(input)
			if err := enc1.Close(); err != nil {
				t.Fatal(err)
			}

			// Encode without concurrent blocks.
			var buf2 bytes.Buffer
			enc2, err := NewWriter(&buf2,
				WithEncoderLevel(level),
				WithEncoderConcurrency(2),
				WithWindowSize(1<<20),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc2.Write(input)
			if err := enc2.Close(); err != nil {
				t.Fatal(err)
			}

			// Both must decode to same content.
			dec1, err := dec.DecodeAll(buf1.Bytes(), nil)
			if err != nil {
				t.Fatal("concurrent decode error:", err)
			}
			dec2, err := dec.DecodeAll(buf2.Bytes(), nil)
			if err != nil {
				t.Fatal("non-concurrent decode error:", err)
			}
			if !bytes.Equal(dec1, dec2) {
				t.Fatalf("decoded content differs: concurrent=%d bytes, non-concurrent=%d bytes",
					len(dec1), len(dec2))
			}
			if !bytes.Equal(dec1, input) {
				t.Fatal("decoded content doesn't match input")
			}
		})
	}
}

func TestConcurrentBlocks_Padding(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for _, padding := range []int{256, 1024, 4096} {
		t.Run(fmt.Sprintf("pad%d", padding), func(t *testing.T) {
			rng := mrand.New(mrand.NewSource(44))
			input := make([]byte, 1<<20)
			for i := range input {
				input[i] = byte(rng.Intn(64))
			}

			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(1<<20),
				WithEncoderPadding(padding),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			if buf.Len()%padding != 0 {
				t.Fatalf("output size %d not aligned to padding %d", buf.Len(), padding)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatal("round-trip mismatch with padding")
			}
		})
	}
}

func TestConcurrentBlocks_DictDisables(t *testing.T) {
	d, err := os.ReadFile("testdata/d0.dict")
	if os.IsNotExist(err) {
		t.Skip("no dict test data")
	}
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewWriter(nil,
		WithConcurrentBlocks(true),
		WithEncoderDict(d),
	)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if enc.o.concurrentBlocks {
		t.Fatal("concurrentBlocks should be disabled when dict is set")
	}
}

func TestConcurrentBlocks_WriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write([]byte("hello"))
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = enc.Write([]byte("world"))
	if !errors.Is(err, ErrEncoderClosed) {
		t.Fatalf("expected ErrEncoderClosed, got %v", err)
	}
}

func TestConcurrentBlocks_MultipleFlushes(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedFastest),
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	rng := mrand.New(mrand.NewSource(66))
	var want bytes.Buffer
	for flush := 0; flush < 5; flush++ {
		chunk := make([]byte, 1<<17)
		for i := range chunk {
			chunk[i] = byte(rng.Intn(32))
		}
		enc.Write(chunk)
		want.Write(chunk)
		if err := enc.Flush(); err != nil {
			t.Fatalf("flush %d error: %v", flush, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, want.Bytes()) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), want.Len())
	}
}

func TestConcurrentBlocks_JobBoundaries(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	const windowSize = 1 << 20 // 1MB -> jobSize = 4MB
	jobSize := windowSize * 4

	rng := mrand.New(mrand.NewSource(77))

	for _, delta := range []int{-1, 0, 1, jobSize} {
		size := jobSize + delta
		if size <= 0 {
			continue
		}
		t.Run(fmt.Sprintf("jobSize%+d", delta), func(t *testing.T) {
			input := make([]byte, size)
			for i := range input {
				input[i] = byte(rng.Intn(64))
			}

			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(windowSize),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatalf("mismatch at size %d", size)
			}
		})
	}
}

func TestConcurrentBlocks_DataPatterns(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	const size = 2 << 20

	patterns := map[string]func([]byte){
		"zeros": func(b []byte) {
			clear(b)
		},
		"0xFF": func(b []byte) {
			for i := range b {
				b[i] = 0xFF
			}
		},
		"incompressible": func(b []byte) {
			rand.Read(b)
		},
		"repetitive": func(b []byte) {
			pat := []byte("ABCDEFGHIJKLMNOP")
			for i := range b {
				b[i] = pat[i%len(pat)]
			}
		},
		"single_byte": func(b []byte) {
			for i := range b {
				b[i] = 'X'
			}
		},
	}

	for name, fill := range patterns {
		t.Run(name, func(t *testing.T) {
			input := make([]byte, size)
			fill(input)

			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(1<<20),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatalf("decode error for %s: %v", name, err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatalf("mismatch for %s: got %d, want %d", name, len(decoded), len(input))
			}
		})
	}
}

func TestConcurrentBlocks_ResetContentSize(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(22))
	input := make([]byte, 1<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	t.Run("correct_size", func(t *testing.T) {
		var buf bytes.Buffer
		enc, err := NewWriter(nil,
			WithEncoderConcurrency(2),
			WithConcurrentBlocks(true),
			WithWindowSize(1<<20),
		)
		if err != nil {
			t.Fatal(err)
		}
		enc.ResetContentSize(&buf, int64(len(input)))
		enc.Write(input)
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}

		decoded, err := dec.DecodeAll(buf.Bytes(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatal("mismatch")
		}
	})

	t.Run("wrong_size", func(t *testing.T) {
		var buf bytes.Buffer
		enc, err := NewWriter(nil,
			WithEncoderConcurrency(2),
			WithConcurrentBlocks(true),
			WithWindowSize(1<<20),
		)
		if err != nil {
			t.Fatal(err)
		}
		enc.ResetContentSize(&buf, int64(len(input)+100))
		enc.Write(input)
		err = enc.Close()
		if err == nil {
			t.Fatal("expected error for wrong content size")
		}
	})
}

func TestConcurrentBlocks_WindowSizes(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(55))

	for _, winSize := range testWindowSizes {
		t.Run(fmt.Sprintf("w%dk", winSize/1024), func(t *testing.T) {
			// Input must exceed jobSize to trigger multi-job.
			jobSize := winSize * 4
			if jobSize < 512<<10 {
				jobSize = 512 << 10
			}
			inputSize := jobSize*2 + 1000
			// Cap at 8MB for test speed.
			if inputSize > 8<<20 {
				inputSize = 8 << 20
			}

			input := make([]byte, inputSize)
			for i := range input {
				input[i] = byte(rng.Intn(64))
			}

			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(winSize),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatalf("mismatch at window %d", winSize)
			}
		})
	}
}

func TestConcurrentBlocks_Concurrent1Fallback(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	input := []byte("concurrent=1 should fallback to sync path")

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(1),
		WithConcurrentBlocks(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: %q != %q", decoded, input)
	}
}

func TestConcurrentBlocks_InterleavedWriteReadFrom(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(33))
	part1 := make([]byte, 1<<18)
	part2 := make([]byte, 1<<18)
	part3 := make([]byte, 1<<18)
	for _, p := range [][]byte{part1, part2, part3} {
		for i := range p {
			p[i] = byte(rng.Intn(64))
		}
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	enc.Write(part1)
	enc.ReadFrom(bytes.NewReader(part2))
	enc.Write(part3)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	var want []byte
	want = append(want, part1...)
	want = append(want, part2...)
	want = append(want, part3...)

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, want) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), len(want))
	}
}

func TestConcurrentBlocks_LowMem(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(11))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
		WithLowerEncoderMem(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatal("mismatch with lowmem")
	}
}

func TestConcurrentBlocks_EntropyOptions(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(44))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	for _, tc := range []struct {
		name string
		opt  EOption
	}{
		{"noentropy", WithNoEntropyCompression(true)},
		{"alllit", WithAllLitEntropyCompression(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderConcurrency(2),
				WithConcurrentBlocks(true),
				WithWindowSize(1<<20),
				tc.opt,
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatalf("mismatch with %s", tc.name)
			}
		})
	}
}

func TestConcurrentBlocks_ManyJobs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping many-jobs stress test in short mode")
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	// MinWindowSize → jobSize=512KB (floor). 4MB input → ~8 jobs.
	rng := mrand.New(mrand.NewSource(99))
	input := make([]byte, 4<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedFastest),
		WithEncoderConcurrency(4),
		WithConcurrentBlocks(true),
		WithWindowSize(MinWindowSize),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), len(input))
	}
}

func TestConcurrentBlocks_EncodeAllAlongside(t *testing.T) {
	rng := mrand.New(mrand.NewSource(42))
	streamInput := make([]byte, 2<<20)
	for i := range streamInput {
		streamInput[i] = byte(rng.Intn(64))
	}

	enc, err := NewWriter(nil,
		WithEncoderConcurrency(4),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	// Start streaming in background.
	var streamBuf bytes.Buffer
	var streamErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		enc.Reset(&streamBuf)
		_, streamErr = enc.Write(streamInput)
		if streamErr != nil {
			return
		}
		streamErr = enc.Close()
	}()

	// Concurrent EncodeAll calls.
	var encAllWg sync.WaitGroup
	for i := 0; i < 20; i++ {
		in := streamInput[rng.Intn(len(streamInput)/2):]
		in = in[:rng.Intn(len(in)/4+1)]
		encAllWg.Add(1)
		go func() {
			defer encAllWg.Done()
			dst := enc.EncodeAll(in, nil)
			decoded, err := dec.DecodeAll(dst, nil)
			if err != nil {
				t.Error("EncodeAll decode error:", err)
				return
			}
			if !bytes.Equal(decoded, in) {
				t.Error("EncodeAll mismatch")
			}
		}()
	}

	encAllWg.Wait()
	wg.Wait()

	if streamErr != nil {
		t.Fatal("stream error:", streamErr)
	}
	decoded, err := dec.DecodeAll(streamBuf.Bytes(), nil)
	if err != nil {
		t.Fatal("stream decode error:", err)
	}
	if !bytes.Equal(decoded, streamInput) {
		t.Fatal("stream mismatch")
	}
}

func TestConcurrentBlocks_IOCopy(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(77))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	n, err := io.Copy(enc, bytes.NewReader(input))
	if err != nil {
		t.Fatal("io.Copy error:", err)
	}
	if n != int64(len(input)) {
		t.Fatalf("io.Copy: got %d, want %d", n, len(input))
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatal("mismatch")
	}
}

func TestConcurrentBlocks_LargeAllLevels(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large multi-level test in short mode")
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(123))
	input := make([]byte, 10<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	levels := []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression}
	for _, level := range levels {
		t.Run(level.String(), func(t *testing.T) {
			var buf bytes.Buffer
			enc, err := NewWriter(&buf,
				WithEncoderLevel(level),
				WithEncoderConcurrency(4),
				WithConcurrentBlocks(true),
				WithWindowSize(1<<20),
			)
			if err != nil {
				t.Fatal(err)
			}
			enc.Write(input)
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatal("mismatch")
			}
			t.Logf("level=%s compressed %d -> %d (%.1f%%)",
				level, len(input), buf.Len(), float64(buf.Len())*100/float64(len(input)))
		})
	}
}

func TestConcurrentBlocks_LargeInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large test in short mode")
	}
	const windowSize = 1 << 20
	const inputSize = 20 << 20

	rng := mrand.New(mrand.NewSource(123))
	input := make([]byte, inputSize)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedDefault),
		WithEncoderConcurrency(4),
		WithConcurrentBlocks(true),
		WithWindowSize(windowSize),
	)
	if err != nil {
		t.Fatal(err)
	}
	n, err := enc.Write(input)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(input) {
		t.Fatalf("short write: %d != %d", n, len(input))
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: got %d bytes, want %d", len(decoded), len(input))
	}
}

func TestConcurrentBlocks_Flush(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedFastest),
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	rng := mrand.New(mrand.NewSource(99))
	part1 := make([]byte, 1<<18)
	part2 := make([]byte, 1<<18)
	for i := range part1 {
		part1[i] = byte(rng.Intn(32))
	}
	for i := range part2 {
		part2[i] = byte(rng.Intn(32))
	}

	enc.Write(part1)
	if err := enc.Flush(); err != nil {
		t.Fatal("flush error:", err)
	}
	enc.Write(part2)
	if err := enc.Close(); err != nil {
		t.Fatal("close error:", err)
	}

	want := append(part1, part2...)
	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, want) {
		t.Fatalf("mismatch after flush: got %d, want %d", len(decoded), len(want))
	}
}

func TestConcurrentBlocks_Reset(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	enc, err := NewWriter(nil,
		WithEncoderLevel(SpeedDefault),
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	rng := mrand.New(mrand.NewSource(77))
	for i := 0; i < 3; i++ {
		var buf bytes.Buffer
		enc.Reset(&buf)

		input := make([]byte, 2<<20)
		for j := range input {
			input[j] = byte(rng.Intn(64))
		}
		enc.Write(input)
		if err := enc.Close(); err != nil {
			t.Fatalf("iter %d close error: %v", i, err)
		}

		decoded, err := dec.DecodeAll(buf.Bytes(), nil)
		if err != nil {
			t.Fatalf("iter %d decode error: %v", i, err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("iter %d mismatch", i)
		}
	}
}

func TestConcurrentBlocks_ReadFrom(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(55))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedFastest),
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	n, err := enc.ReadFrom(bytes.NewReader(input))
	if err != nil {
		t.Fatal("ReadFrom error:", err)
	}
	if n != int64(len(input)) {
		t.Fatalf("ReadFrom: got %d, want %d", n, len(input))
	}
	if err := enc.Close(); err != nil {
		t.Fatal("close error:", err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), len(input))
	}
}

func TestConcurrentBlocks_Empty(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for _, fullZero := range []bool{true, false} {
		var buf bytes.Buffer
		enc, err := NewWriter(&buf,
			WithEncoderConcurrency(2),
			WithConcurrentBlocks(true),
			WithZeroFrames(fullZero),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		if fullZero {
			if buf.Len() == 0 {
				t.Fatal("expected non-empty output with fullZero")
			}
			decoded, err := dec.DecodeAll(buf.Bytes(), nil)
			if err != nil {
				t.Fatal("decode error:", err)
			}
			if len(decoded) != 0 {
				t.Fatalf("expected empty decoded, got %d bytes", len(decoded))
			}
		} else {
			if buf.Len() != 0 {
				t.Fatalf("expected no output without fullZero, got %d bytes", buf.Len())
			}
		}
	}
}

func TestConcurrentBlocks_SmallWrite(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("hello world")
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: %q != %q", decoded, input)
	}
}

func TestConcurrentBlocks_ManySmallWrites(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedFastest),
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}

	rng := mrand.New(mrand.NewSource(33))
	var want bytes.Buffer
	chunk := make([]byte, 1024)
	for i := 0; i < 8192; i++ {
		for j := range chunk {
			chunk[j] = byte(rng.Intn(32))
		}
		enc.Write(chunk)
		want.Write(chunk)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, want.Bytes()) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), want.Len())
	}
}

func TestConcurrentBlocks_StreamDecode(t *testing.T) {
	rng := mrand.New(mrand.NewSource(11))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderLevel(SpeedDefault),
		WithEncoderConcurrency(3),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	dec, err := NewReader(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	decoded, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal("stream decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatalf("mismatch: got %d, want %d", len(decoded), len(input))
	}
}

func TestConcurrentBlocks_NoCRC(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	rng := mrand.New(mrand.NewSource(22))
	input := make([]byte, 2<<20)
	for i := range input {
		input[i] = byte(rng.Intn(64))
	}

	var buf bytes.Buffer
	enc, err := NewWriter(&buf,
		WithEncoderConcurrency(2),
		WithConcurrentBlocks(true),
		WithWindowSize(1<<20),
		WithEncoderCRC(false),
	)
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(input)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	decoded, err := dec.DecodeAll(buf.Bytes(), nil)
	if err != nil {
		t.Fatal("decode error:", err)
	}
	if !bytes.Equal(decoded, input) {
		t.Fatal("mismatch without CRC")
	}
}

func BenchmarkConcurrentBlocks(b *testing.B) {
	rng := mrand.New(mrand.NewSource(0))
	input := make([]byte, 10<<20)
	for i := range input {
		input[i] = byte(rng.Intn(256))
	}

	for _, conc := range []int{1, 2, 4} {
		for _, cb := range []bool{false, true} {
			name := fmt.Sprintf("conc%d", conc)
			if cb {
				name += "-parallel"
			}
			if conc < 2 && cb {
				continue
			}
			b.Run(name, func(b *testing.B) {
				enc, err := NewWriter(io.Discard,
					WithEncoderLevel(SpeedDefault),
					WithEncoderConcurrency(conc),
					WithConcurrentBlocks(cb),
					WithWindowSize(1<<20),
				)
				if err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				for i := 0; i < b.N; i++ {
					enc.Reset(io.Discard)
					_, err := enc.Write(input)
					if err != nil {
						b.Fatal(err)
					}
					err = enc.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkConcurrentBlocks_GobStream(b *testing.B) {
	input, err := os.ReadFile("testdata/gob-stream")
	if err != nil {
		b.Skip("testdata/gob-stream not available:", err)
	}

	levels := []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression}
	for _, level := range levels {
		for _, conc := range []int{1, 4, 16} {
			for _, cb := range []bool{false, true} {
				if conc < 2 && cb {
					continue
				}
				name := fmt.Sprintf("%s-c%d", level, conc)
				if cb {
					name += "-parallel"
				}
				b.Run(name, func(b *testing.B) {
					var cw countWriter
					enc, err := NewWriter(&cw,
						WithEncoderLevel(level),
						WithEncoderConcurrency(conc),
						WithConcurrentBlocks(cb),
					)
					if err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					b.ReportAllocs()
					b.SetBytes(int64(len(input)))
					for i := 0; i < b.N; i++ {
						cw.n = 0
						enc.Reset(&cw)
						_, err := enc.Write(input)
						if err != nil {
							b.Fatal(err)
						}
						if err := enc.Close(); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(cw.n), "outBytes")
					b.ReportMetric(float64(cw.n)*100/float64(len(input)), "pct")
				})
			}
		}
	}
}
