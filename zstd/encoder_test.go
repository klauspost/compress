// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/klauspost/compress/huff0"
	"github.com/klauspost/compress/internal/silesiatest"
	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd/internal/xxhash"
)

var testWindowSizes = []int{MinWindowSize, 1 << 16, 1 << 22, 1 << 24}

// The ultra level is slow, so it gets a reduced test matrix.
var (
	ultraTestWindowSizes = []int{1 << 16, 8 << 20}
	ultraTestOpts        = map[string]bool{"default": true, "lowmem": true, "nolit": true, "1seg": true, "conc": true}
)

type testEncOpt struct {
	name  string
	level EncoderLevel
	o     []EOption
}

// testWindows returns the window sizes to test level with.
func testWindows(level EncoderLevel) []int {
	if level == SpeedUltraCompression {
		return ultraTestWindowSizes
	}
	return testWindowSizes
}

func getEncOpts(cMax int) []testEncOpt {
	var o []testEncOpt
	for level := speedNotSet + 1; level < speedLast; level++ {
		if isRaceTest && level >= SpeedBestCompression {
			break
		}
		for conc := 1; conc <= 4; conc *= 2 {
			if level == SpeedUltraCompression && conc > 2 {
				break
			}
			for _, wind := range testWindows(level) {
				addOpt := func(name string, options ...EOption) {
					if level == SpeedUltraCompression && !ultraTestOpts[name] {
						return
					}
					opts := append([]EOption(nil), WithEncoderLevel(level), WithEncoderConcurrency(conc), WithWindowSize(wind))
					name = fmt.Sprintf("%s-c%d-w%dk-%s", level.String(), conc, wind/1024, name)
					o = append(o, testEncOpt{name: name, level: level, o: append(opts, options...)})
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
				addOpt("1seg", WithSingleSegment(true))
				addOpt("conc", WithConcurrentBlocks(true))
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

func TestEncoder_EncodeAllSimple(t *testing.T) {
	in, err := os.ReadFile("testdata/z000028")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	in = append(in, in...)
	for _, opts := range getEncOpts(4) {
		t.Run(opts.name, func(t *testing.T) {
			runtime.GC()
			e, err := NewWriter(nil, opts.o...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			start := time.Now()
			dst := e.EncodeAll(in, nil)
			//t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
			mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
			t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

			decoded, err := dec.DecodeAll(dst, nil)
			if err != nil {
				t.Error(err, len(decoded))
			}
			if !bytes.Equal(decoded, in) {
				os.WriteFile("testdata/"+t.Name()+"-z000028.got", decoded, os.ModePerm)
				os.WriteFile("testdata/"+t.Name()+"-z000028.want", in, os.ModePerm)
				t.Fatal("Decoded does not match")
			}
			//t.Log("Encoded content matched")
		})
	}
}

func TestEncoder_EncodeAllConcurrent(t *testing.T) {
	in, err := os.ReadFile("testdata/z000028")
	if err != nil {
		t.Fatal(err)
	}
	in = append(in, in...)

	// When running race no more than 8k goroutines allowed.
	n := 400 / runtime.GOMAXPROCS(0)
	if testing.Short() {
		n = 20 / runtime.GOMAXPROCS(0)
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for _, opts := range getEncOpts(2) {
		t.Run(opts.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(0x1337))
			e, err := NewWriter(nil, opts.o...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			n := n
			if opts.level == SpeedUltraCompression {
				n = min(n, 8)
			}
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				in := in[rng.Int()&1023:]
				in = in[:rng.Intn(len(in))]
				go func() {
					defer wg.Done()
					dst := e.EncodeAll(in, nil)
					if len(dst) > e.MaxEncodedSize(len(in)) {
						t.Errorf("max encoded size for %v: got: %d, want max: %d", len(in), len(dst), e.MaxEncodedSize(len(in)))
					}
					//t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
					decoded, err := dec.DecodeAll(dst, nil)
					if err != nil {
						t.Error(err, len(decoded))
					}
					if !bytes.Equal(decoded, in) {
						//os.WriteFile("testdata/"+t.Name()+"-z000028.got", decoded, os.ModePerm)
						//os.WriteFile("testdata/"+t.Name()+"-z000028.want", in, os.ModePerm)
						t.Error("Decoded does not match")
						return
					}
				}()
			}
			wg.Wait()
			//t.Log("Encoded content matched.", n, "goroutines")
		})
	}
}

func TestEncoder_EncodeAllEncodeXML(t *testing.T) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		in = in[:10000]
	}

	for level := speedNotSet + 1; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			if isRaceTest && level >= SpeedBestCompression {
				t.SkipNow()
			}
			in := in
			if level == SpeedUltraCompression {
				in = in[:min(len(in), 512<<10)]
			}
			e, err := NewWriter(nil, WithEncoderLevel(level))
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			start := time.Now()
			dst := e.EncodeAll(in, nil)
			if len(dst) > e.MaxEncodedSize(len(in)) {
				t.Errorf("max encoded size for %v: got: %d, want max: %d", len(in), len(dst), e.MaxEncodedSize(len(in)))
			}
			//t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
			mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
			t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

			decoded, err := dec.DecodeAll(dst, nil)
			if err != nil {
				t.Error(err, len(decoded))
			}
			if !bytes.Equal(decoded, in) {
				os.WriteFile("testdata/"+t.Name()+"-xml.got", decoded, os.ModePerm)
				t.Error("Decoded does not match")
				return
			}
			//t.Log("Encoded content matched")
		})
	}
}

// TestEncoderLongOffsetSequence makes a sequence with more than 56 extra
// bits (32K+ literals, 64K+ match, offset over 2^26), which the sequence
// encoder has to split.
func TestEncoderLongOffsetSequence(t *testing.T) {
	if testing.Short() || isRaceTest {
		t.Skip("large input")
	}
	const window = 1 << 27
	rng := rand.New(rand.NewSource(1))
	a := make([]byte, 100_000)
	rng.Read(a)
	lits := make([]byte, 32_800)
	rng.Read(lits)
	// Start the literals on a block boundary so the sequence fits in one
	// block, and end with a short match: the last sequence is encoded apart.
	in := make([]byte, 513*maxCompressedBlockSize, 513*maxCompressedBlockSize+len(lits)+98_100)
	copy(in, a)
	in = append(in, lits...)
	in = append(in, a[:98_000]...)
	in = append(in, lits[:100]...)

	dec, err := NewReader(nil, WithDecoderMaxWindow(window))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for level := speedNotSet + 1; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			enc, err := NewWriter(nil, WithEncoderLevel(level), WithWindowSize(window), WithEncoderConcurrency(1))
			if err != nil {
				t.Fatal(err)
			}
			defer enc.Close()
			got, err := dec.DecodeAll(enc.EncodeAll(in, nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, in) {
				t.Fatal("decoded does not match")
			}
		})
	}
}

func appendRandom(dst []byte, rng *rand.Rand, n int) []byte {
	dst = append(dst, make([]byte, n)...)
	rng.Read(dst[len(dst)-n:])
	return dst
}

// appendPeriodic appends n bytes that repeat the previous period bytes,
// with about one random change per period.
func appendPeriodic(dst []byte, rng *rand.Rand, n, period int) []byte {
	for range n {
		c := dst[len(dst)-period]
		if rng.Intn(period) == 0 {
			c = byte(rng.Intn(256))
		}
		dst = append(dst, c)
	}
	return dst
}

// TestEncoderInPlace checks that encoding blocks where they are gives the
// same output as copying them into the history, also after the copy has moved
// the window down.
func TestEncoderInPlace(t *testing.T) {
	initPredefined()
	const block = 64 << 10
	n := 2 << 20
	if testing.Short() {
		n = 512 << 10
	}
	rng := rand.New(rand.NewSource(1))
	src := appendRandom(nil, rng, 4<<10)
	for len(src) < n {
		// Short and long distances, some past the 256 KiB window.
		dist := 1 + rng.Intn(min(len(src), 64<<10<<rng.Intn(5)))
		src = appendPeriodic(src, rng, rng.Intn(4<<10), dist)
		src = appendRandom(src, rng, rng.Intn(1<<10))
	}
	for _, level := range []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression, SpeedUltraCompression} {
		for _, lowMem := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v-lowmem-%v", level, lowMem), func(t *testing.T) {
				var o encoderOptions
				o.setDefault()
				o.level, o.windowSize, o.lowMem = level, 256<<10, lowMem
				ref, enc := o.encoder(), o.encoder()
				ref.Reset(nil, false)
				enc.Reset(nil, false)
				enc.inPlace(src[:0])
				for i := 0; i < len(src); i += block {
					b := src[i:min(i+block, len(src))]
					var out [2][]byte
					for j, e := range [2]encoder{ref, enc} {
						blk := e.Block()
						blk.pushOffsets()
						e.Encode(blk, b)
						blk.last = i+block >= len(src)
						if err := blk.encode(b, false, true); err != nil {
							t.Fatal(err)
						}
						out[j] = bytes.Clone(blk.output)
						blk.reset(nil)
					}
					if !bytes.Equal(out[0], out[1]) {
						t.Fatalf("block at %d differs", i)
					}
				}
			})
		}
	}
}

// encoderBase returns the shared state of enc and the positions in its match
// tables.
func encoderBase(enc encoder) (*fastBase, []int32) {
	var pos []int32
	switch e := enc.(type) {
	case *fastEncoder:
		for _, v := range e.table {
			pos = append(pos, v.offset)
		}
		return &e.fastBase, pos
	case *doubleFastEncoder:
		for _, v := range e.table {
			pos = append(pos, v.offset)
		}
		for _, v := range e.longTable {
			pos = append(pos, v.offset)
		}
		return &e.fastBase, pos
	case *betterFastEncoder:
		for _, v := range e.table {
			pos = append(pos, v.offset)
		}
		for _, v := range e.longTable {
			pos = append(pos, v.offset, v.prev)
		}
		return &e.fastBase, pos
	case *bestFastEncoder:
		for _, v := range e.table {
			pos = append(pos, v.offset, v.prev)
		}
		for _, v := range e.longTable {
			pos = append(pos, v.offset, v.prev)
		}
		return &e.fastBase, pos
	}
	panic(fmt.Sprintf("unexpected encoder %T", enc))
}

// TestEncoderPrefixRestart encodes a job in place whose history ends at the
// position limit. The next prefix is shorter than the window, so shifting
// the tables would keep entries from just before it, and positions must
// restart instead. Ultra starts frames above all old positions instead.
func TestEncoderPrefixRestart(t *testing.T) {
	initPredefined()
	const window, prefix, job, block = 64 << 10, 32 << 10, 256 << 10, 64 << 10
	rng := rand.New(rand.NewSource(1))
	src := appendRandom(nil, rng, 4<<10)
	for len(src) < prefix+job+block {
		src = appendPeriodic(src, rng, rng.Intn(4<<10), 1+rng.Intn(min(len(src), window)))
		src = appendRandom(src, rng, rng.Intn(1<<10))
	}
	for _, level := range []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression} {
		t.Run(level.String(), func(t *testing.T) {
			var o encoderOptions
			o.setDefault()
			o.level, o.windowSize = level, window
			enc := o.encoder()
			base, _ := encoderBase(enc)
			// A new encoder puts the first prefix at the window, so this
			// job's history ends at the limit.
			base.bufferReset = window + prefix + job
			encode := func(hist, blocks []byte) {
				enc.ResetPrefix(hist)
				blk := enc.Block()
				for i := 0; i < len(blocks); i += block {
					blk.pushOffsets()
					enc.Encode(blk, blocks[i:min(i+block, len(blocks))])
					blk.reset(nil)
				}
			}
			encode(src[:prefix:prefix+job], src[prefix:prefix+job])
			enc.inPlace(nil)
			end := prefix + job
			encode(src[job:end:end+block], src[end:end+block])
			_, pos := encoderBase(enc)
			for _, p := range pos {
				if p > base.cur-base.maxMatchOff && p < base.cur {
					t.Fatalf("position %d is within the window before the history at %d", p, base.cur)
				}
			}
		})
	}
}

// TestEncoderUltraBlockReps checks that ultra starts blocks after the first
// without repeat offsets: the decoder only learns the offsets of a block if
// the block is written with its sequences, which is decided after parsing.
func TestEncoderUltraBlockReps(t *testing.T) {
	// The window limits blocks to 64 KiB.
	const block = 64 << 10
	rng := rand.New(rand.NewSource(1))
	// Each random block ends in a short match too costly to code, so the
	// block is written raw. The next block repeats its offset after one literal.
	var in []byte
	for range 2 {
		in = appendRandom(in, rng, block-20)
		for range 8 {
			in = append(in, in[len(in)-1000])
		}
		in = appendRandom(in, rng, 13)
		for range 200 {
			in = append(in, in[len(in)-1000])
		}
		in = appendRandom(in, rng, block-201)
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	newEnc := func(t *testing.T, opts ...EOption) *Encoder {
		e, err := NewWriter(nil, append([]EOption{WithEncoderLevel(SpeedUltraCompression), WithWindowSize(block)}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	stream := func(t *testing.T, e *Encoder, in []byte) []byte {
		var buf bytes.Buffer
		e.Reset(&buf)
		if _, err := e.Write(in); err != nil {
			t.Fatal(err)
		}
		if err := e.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	check := func(t *testing.T, enc, want []byte) {
		t.Helper()
		got, err := dec.DecodeAll(enc, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatal("decoded does not match")
		}
	}
	// The rule is only tested when a raw block is followed by sequences.
	checkRaw := func(t *testing.T, enc, want []byte) {
		t.Helper()
		check(t, enc, want)
		lt, seqs := blockTypes(t, enc, true)
		for i := 1; i < len(lt); i++ {
			if lt[i-1] == -1 && seqs[i] {
				return
			}
		}
		t.Fatalf("no raw block before sequences: literal types %v, sequences %v", lt, seqs)
	}
	t.Run("sync", func(t *testing.T) {
		checkRaw(t, stream(t, newEnc(t, WithEncoderConcurrency(1)), in), in)
	})
	t.Run("async", func(t *testing.T) {
		checkRaw(t, stream(t, newEnc(t, WithEncoderConcurrency(2)), in), in)
	})
	t.Run("encodeall", func(t *testing.T) {
		checkRaw(t, newEnc(t).EncodeAll(in, nil), in)
	})
	t.Run("jobs", func(t *testing.T) {
		// Two jobs.
		in := bytes.Repeat(in, 3)
		checkRaw(t, stream(t, newEnc(t, WithEncoderConcurrency(2), WithConcurrentBlocks(true)), in), in)
	})
	t.Run("jobs-reuse", func(t *testing.T) {
		// Two empty jobs leave both pooled encoders reset for a frame but
		// unused, so the next stream's second job gets one whichever worker
		// takes an encoder first.
		e := newEnc(t, WithEncoderConcurrency(2), WithConcurrentBlocks(true), WithZeroFrames(true))
		for range 2 {
			check(t, stream(t, e, nil), nil)
			if e.state.jobs.jobSeq != 1 {
				t.Fatal("no job for the empty stream")
			}
		}
		// The first job ends with periodic data, the second starts with a run.
		in := appendRandom(nil, rng, e.o.jobSize()-block)
		in = appendPeriodic(in, rng, block, 37)
		in = append(in, 'x')
		in = append(in, bytes.Repeat([]byte{'a'}, 300)...)
		in = appendRandom(in, rng, 1000)
		check(t, stream(t, e, in), in)
	})
}

// forEachEncoder replaces each free encoder of e with what f returns for it.
// It takes them all before putting any back, as the pool hands out the last
// one put back.
func forEachEncoder(e *Encoder, f func(encoder) encoder) {
	e.init.Do(e.initialize)
	encs := make([]encoder, 0, e.encoders.Len())
	for range e.encoders.Len() {
		enc, _ := e.encoders.Get()
		encs = append(encs, enc)
	}
	for _, enc := range encs {
		e.encoders.Put(f(enc))
	}
}

// setUltraBufferReset sets the position limit of e's ultra encoders.
func setUltraBufferReset(e *Encoder, limit int32) {
	forEachEncoder(e, func(enc encoder) encoder {
		enc.(*ultraEncoder).bufferReset = limit
		return enc
	})
	e.state.encoder.(*ultraEncoder).bufferReset = limit
}

// TestEncoderUltraRebase lowers the position limit of ultra encoders, so they
// clear their tables between frames and rebase them within frames.
// Positions are only compared to each other, so the output must not change.
func TestEncoderUltraRebase(t *testing.T) {
	const window = 64 << 10
	rng := rand.New(rand.NewSource(1))
	// Random runs and copies of earlier data from up to a window back.
	in := make([]byte, 0, 1<<20)
	for len(in) < cap(in) {
		n := min(16+rng.Intn(2048), cap(in)-len(in))
		if len(in) < window || rng.Intn(4) == 0 {
			in = appendRandom(in, rng, n)
			continue
		}
		for i := len(in) - 1 - rng.Intn(window-1); n > 0; n-- {
			in = append(in, in[i])
			i++
		}
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	// check runs f with opts, then again for each position limit,
	// and checks that the outputs are equal and decode to want.
	check := func(t *testing.T, dec *Decoder, limits []int32, want []byte, f func(e *Encoder) []byte, opts ...EOption) {
		t.Helper()
		var ref []byte
		for i, limit := range append([]int32{0}, limits...) {
			e, err := NewWriter(io.Discard, append([]EOption{WithEncoderLevel(SpeedUltraCompression)}, opts...)...)
			if err != nil {
				t.Fatal(err)
			}
			if limit > 0 {
				setUltraBufferReset(e, limit)
			}
			got := f(e)
			if i == 0 {
				ref = got
				continue
			}
			if !bytes.Equal(got, ref) {
				t.Fatalf("limit %d changed the output: %d bytes, want %d", limit, len(got), len(ref))
			}
			dgot, err := dec.DecodeAll(got, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dgot, want) {
				t.Fatal("decoded does not match")
			}
		}
	}
	// The first limit is reached within frames that start at unaligned
	// positions. The second limit resets most frames and rebases almost every block.
	limits := []int32{512 << 10, 256 << 10}
	// sizes are frame or write sizes on both sides of the block size.
	sizes := []int{1 << 10, 100 << 10, 700 << 10, 7, window, window + 1, 300 << 10}
	split := func(b []byte, f func(i int, b []byte)) {
		for i := 0; len(b) > 0; i++ {
			n := min(sizes[i%len(sizes)], len(b))
			f(i, b[:n])
			b = b[n:]
		}
	}
	t.Run("stream", func(t *testing.T) {
		check(t, dec, limits, in, func(e *Encoder) []byte {
			var buf bytes.Buffer
			split(in, func(_ int, b []byte) {
				e.Reset(&buf)
				if _, err := e.Write(b); err != nil {
					t.Fatal(err)
				}
				if err := e.Close(); err != nil {
					t.Fatal(err)
				}
			})
			return buf.Bytes()
		}, WithWindowSize(window), WithEncoderConcurrency(1))
	})
	t.Run("encodeall", func(t *testing.T) {
		check(t, dec, limits, in, func(e *Encoder) []byte {
			var out []byte
			split(in, func(_ int, b []byte) { out = e.EncodeAll(b, out) })
			return out
		}, WithWindowSize(window), WithEncoderConcurrency(1))
	})
	t.Run("dict", func(t *testing.T) {
		// Each frame is a prefix of src, which starts with copies from the dictionary.
		dict, src := in[:window], in[window:]
		dec, err := NewReader(nil, WithDecoderDictRaw(1, dict))
		if err != nil {
			t.Fatal(err)
		}
		defer dec.Close()
		var want []byte
		split(src, func(_ int, b []byte) { want = append(want, src[:len(b)]...) })
		check(t, dec, limits, want, func(e *Encoder) []byte {
			var out []byte
			split(src, func(_ int, b []byte) { out = e.EncodeAll(src[:len(b)], out) })
			return out
		}, WithWindowSize(window), WithEncoderConcurrency(1), WithEncoderDictRaw(1, dict))
	})
	t.Run("jobs", func(t *testing.T) {
		check(t, dec, limits, in, func(e *Encoder) []byte {
			var buf bytes.Buffer
			e.Reset(&buf)
			split(in, func(i int, b []byte) {
				if _, err := e.Write(b); err != nil {
					t.Fatal(err)
				}
				if i%3 == 2 {
					if err := e.Flush(); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			return buf.Bytes()
		}, WithWindowSize(window), WithEncoderConcurrency(2), WithConcurrentBlocks(true))
	})
	t.Run("window", func(t *testing.T) {
		// The window spans both frames, so a rebase in the second frame
		// keeps all of it. The limit is reached in the first copy of the
		// second frame, so the second copy matches data from before the rebase.
		frames := [][]byte{in[:512<<10], bytes.Repeat(appendRandom(nil, rng, 256<<10), 2)}
		var spy *ultraRebaseSpy
		var size int
		check(t, dec, []int32{1 << 20}, bytes.Join(frames, nil), func(e *Encoder) []byte {
			e.init.Do(e.initialize)
			enc, _ := e.encoders.Get()
			spy = &ultraRebaseSpy{ultraEncoder: enc.(*ultraEncoder)}
			e.encoders.Put(spy)
			out := e.EncodeAll(frames[0], nil)
			spy.rebased = false
			n := len(out)
			out = e.EncodeAll(frames[1], out)
			size = len(out) - n
			return out
		}, WithWindowSize(1<<28), WithLowerEncoderMem(true), WithEncoderConcurrency(1))
		if !spy.rebased {
			t.Fatal("no rebase within the second frame")
		}
		if size > len(frames[1])*9/16 {
			t.Fatalf("second frame is %d bytes; was the repeat found?", size)
		}
	})
}

// ultraRebaseSpy records whether a block after the first lowered positions.
type ultraRebaseSpy struct {
	*ultraEncoder
	rebased bool
}

func (s *ultraRebaseSpy) Encode(blk *blockEnc, src []byte) {
	first := len(s.hist) == 0
	pos := s.cur + int32(len(s.hist))
	s.ultraEncoder.Encode(blk, src)
	// After the first block, positions grow by len(src) unless rebased.
	s.rebased = s.rebased || !first && s.cur+int32(len(s.hist)) < pos+int32(len(src))
}

// TestEncoderUltraFresh encodes the first frame of new ultra encoders from data
// whose lookups mostly find empty table entries, which read as position 0.
func TestEncoderUltraFresh(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	in := appendRandom(nil, rng, 300<<10)
	for i := 64; i < len(in)-16; i += 1 + rng.Intn(1024) {
		copy(in[i:i+3+rng.Intn(12)], in[rng.Intn(i-16):])
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for _, size := range []int{9, 1000, 16<<10 + 1, 200 << 10, len(in)} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprint(size, "-stream-", stream), func(t *testing.T) {
				in := in[:size]
				var buf bytes.Buffer
				e, err := NewWriter(&buf, WithEncoderLevel(SpeedUltraCompression), WithEncoderConcurrency(1))
				if err != nil {
					t.Fatal(err)
				}
				if stream {
					if _, err := e.Write(in); err != nil {
						t.Fatal(err)
					}
					if err := e.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					buf.Write(e.EncodeAll(in, nil))
				}
				got, err := dec.DecodeAll(buf.Bytes(), nil)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, in) {
					t.Fatal("decoded does not match")
				}
			})
		}
	}
}

// TestEncoderUltraChanges encodes input whose statistics change within a block.
// Where ultra rescales its statistics must depend only on the encoder, since an
// async stream parses a block before the block before it is written.
func TestEncoderUltraChanges(t *testing.T) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	// The last block is not full.
	in = in[:768<<10-1000]
	if (&ultraWork{}).splitAt(in[4<<17:5<<17]) == 1<<17 {
		t.Fatal("the fifth block does not change")
	}
	var want []byte
	for _, path := range []string{"encodeall", "sync", "async"} {
		t.Run(path, func(t *testing.T) {
			conc := 2
			if path == "sync" {
				conc = 1
			}
			e, err := NewWriter(nil, WithEncoderLevel(SpeedUltraCompression), WithEncoderConcurrency(conc))
			if err != nil {
				t.Fatal(err)
			}
			var comp []byte
			if path == "encodeall" {
				comp = e.EncodeAll(in, nil)
			} else {
				var buf bytes.Buffer
				e.ResetContentSize(&buf, int64(len(in)))
				if _, err := e.Write(in); err != nil {
					t.Fatal(err)
				}
				if err := e.Close(); err != nil {
					t.Fatal(err)
				}
				comp = buf.Bytes()
			}
			if len(comp) > e.MaxEncodedSize(len(in)) {
				t.Fatalf("%d bytes, more than MaxEncodedSize", len(comp))
			}
			got, err := dec.DecodeAll(comp, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, in) {
				t.Fatal("decoded does not match")
			}
			// Only the frame headers differ.
			var h Header
			if err := h.Decode(comp); err != nil {
				t.Fatal(err)
			}
			if want == nil {
				want = comp[h.HeaderSize:]
			} else if !bytes.Equal(comp[h.HeaderSize:], want) {
				t.Fatal("blocks differ from the first path")
			}
		})
	}
}

func TestEncoderRegression(t *testing.T) {
	defer timeout(4 * time.Minute)()
	data, err := os.ReadFile("testdata/comp-crashers.zip")
	if err != nil {
		t.Fatal(err)
	}
	// We can't close the decoder.
	dec, err := NewReader(nil)
	if err != nil {
		t.Error(err)
		return
	}
	defer dec.Close()
	ultraRuns := 0
	for _, opts := range getEncOpts(2) {
		// One configuration covers the slow ultra level.
		if opts.level == SpeedUltraCompression {
			if ultraRuns++; ultraRuns > 1 {
				continue
			}
		}
		t.Run(opts.name, func(t *testing.T) {
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			enc, err := NewWriter(
				nil,
				opts.o...,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer enc.Close()

			for i, tt := range zr.File {
				if !strings.HasSuffix(t.Name(), "") {
					continue
				}
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
					if opts.level == SpeedUltraCompression && len(in) > 256<<10 {
						t.SkipNow()
					}
					encoded := enc.EncodeAll(in, nil)
					if len(encoded) > enc.MaxEncodedSize(len(in)) {
						t.Errorf("max encoded size for %v: got: %d, want max: %d", len(in), len(encoded), enc.MaxEncodedSize(len(in)))
					}
					// Usually too small...
					got, err := dec.DecodeAll(encoded, make([]byte, 0, len(in)))
					if err != nil {
						t.Logf("error: %v\nwant: %v\ngot:  %v", err, len(in), len(got))
						t.Fatal(err)
					}
					// Use the Writer
					var dst bytes.Buffer
					enc.ResetContentSize(&dst, int64(len(in)))
					_, err = enc.Write(in)
					if err != nil {
						t.Error(err)
					}
					err = enc.Close()
					if err != nil {
						t.Error(err)
					}
					err = enc.Close()
					if err != nil {
						t.Error(err)
					}
					_, err = enc.Write([]byte{1, 2, 3, 4})
					if !errors.Is(err, ErrEncoderClosed) {
						t.Errorf("unexpected error: %v", err)
					}
					encoded = dst.Bytes()
					if len(encoded) > enc.MaxEncodedSize(len(in)) {
						t.Errorf("max encoded size for %v: got: %d, want max: %d", len(in), len(encoded), enc.MaxEncodedSize(len(in)))
					}
					got, err = dec.DecodeAll(encoded, make([]byte, 0, len(in)/2))
					if err != nil {
						t.Logf("error: %v\nwant: %v\ngot:  %v", err, len(in), len(got))
						t.Error(err)
					}
				})
			}
		})
	}
}

func TestEncoder_EncodeAllTwain(t *testing.T) {
	in, err := os.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt")
	if err != nil {
		t.Fatal(err)
	}
	testWindowSizes := testWindowSizes
	if testing.Short() {
		testWindowSizes = []int{1 << 20}
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for level := speedNotSet + 1; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			if isRaceTest && level >= SpeedBestCompression {
				t.SkipNow()
			}
			for _, windowSize := range testWindowSizes {
				t.Run(fmt.Sprintf("window:%d", windowSize), func(t *testing.T) {
					e, err := NewWriter(nil, WithEncoderLevel(level), WithWindowSize(windowSize))
					if err != nil {
						t.Fatal(err)
					}
					defer e.Close()
					start := time.Now()
					dst := e.EncodeAll(in, nil)
					t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
					mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
					t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

					decoded, err := dec.DecodeAll(dst, nil)
					if err != nil {
						t.Error(err, len(decoded))
					}
					if !bytes.Equal(decoded, in) {
						os.WriteFile("testdata/"+t.Name()+"-Mark.Twain-Tom.Sawyer.txt.got", decoded, os.ModePerm)
						t.Fatal("Decoded does not match")
					}
					t.Log("Encoded content matched")
				})
			}
		})
	}
}

func TestEncoder_EncodeRLE(t *testing.T) {
	in := make([]byte, 1<<20)
	testWindowSizes := testWindowSizes
	if testing.Short() {
		testWindowSizes = []int{1 << 20}
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for level := speedNotSet + 1; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			if isRaceTest && level >= SpeedBestCompression {
				t.SkipNow()
			}
			for _, windowSize := range testWindowSizes {
				t.Run(fmt.Sprintf("window:%d", windowSize), func(t *testing.T) {
					e, err := NewWriter(nil, WithEncoderLevel(level), WithWindowSize(windowSize))
					if err != nil {
						t.Fatal(err)
					}
					defer e.Close()
					start := time.Now()
					dst := e.EncodeAll(in, nil)
					t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
					mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
					t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

					decoded, err := dec.DecodeAll(dst, nil)
					if err != nil {
						t.Error(err, len(decoded))
					}
					if !bytes.Equal(decoded, in) {
						os.WriteFile("testdata/"+t.Name()+"-RLE.got", decoded, os.ModePerm)
						t.Fatal("Decoded does not match")
					}
					t.Log("Encoded content matched")
					// Ultra parses these blocks, so its output must be turned back into RLE blocks.
					if level == SpeedUltraCompression && len(dst) > 18+4*len(in)/e.o.blockSize {
						t.Errorf("got %d bytes, want RLE blocks", len(dst))
					}
				})
			}
		})
	}
}

// In these streams a block builds a literal table but stores its literals raw,
// and the next block would reuse the table the decoder never got.
func TestEncoderFlushRawLiterals(t *testing.T) {
	streams := [][]string{
		{"ahbkkdaflabkbbecakeiaaddaaaaacjaajadbbfalbkmfaamalfkja", "mmiijagdaafcbaajcmjdbalcmgbcagafbijfakbabgcfdkcaeldgfik", "akecbajaaaaahfabgahc", "jkcbaemekbmibgaffdegkdlifhaabiaaca", "bcjihjimfaemceeamhkabgebaaacaaaafaaback"},
		{"kbeaelahfejbdfeediaenceadfdgijlbfbabbndikkdal", "jmgcahcbkffagbdbccnoejcdlemeahaana", "aaealoaanmkabahkbfgmdccgcagnacfjafa", "onfaneoanaghbacbeckanaeacomfchfgabadgnhnieeliggccginaaoelgkbdelnaheaciaakibbakafihbadkdkdlhcibgibldkaanhe", "ahcehdkdlnbjgaacdonlkmabhbhaajacehjcokaf"},
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var buf bytes.Buffer
	enc, err := NewWriter(&buf, WithEncoderLevel(SpeedBetterCompression))
	if err != nil {
		t.Fatal(err)
	}
	for i, pieces := range streams {
		buf.Reset()
		enc.Reset(&buf)
		var in []byte
		for _, p := range pieces {
			in = append(in, p...)
			if _, err := enc.Write([]byte(p)); err != nil {
				t.Fatal(err)
			}
			if err := enc.Flush(); err != nil {
				t.Fatal(err)
			}
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := dec.DecodeAll(buf.Bytes(), nil)
		if err != nil {
			t.Errorf("stream %d: %v", i, err)
		} else if !bytes.Equal(got, in) {
			t.Errorf("stream %d: decoded does not match", i)
		}
	}
}

// TestEncoderLiteralsBound encodes blocks of literals that huff0 codes to 5
// bytes less, which a 5-byte literals header and the sequence count make 1 byte
// larger than raw: more than a full block may hold.
func TestEncoderLiteralsBound(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for _, level := range []EncoderLevel{SpeedBestCompression, SpeedUltraCompression} {
		// A full block, and a full block of a 32 KiB window.
		for _, n := range []int{maxCompressedBlockSize, 32 << 10} {
			in := huffTunedLits(t, n, level == SpeedUltraCompression)
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprint(level, "-", n, "-", stream), func(t *testing.T) {
					opts := []EOption{WithEncoderLevel(level), WithEncoderConcurrency(2)}
					if n < maxCompressedBlockSize {
						opts = append(opts, WithWindowSize(n))
					}
					var buf bytes.Buffer
					enc, err := NewWriter(&buf, opts...)
					if err != nil {
						t.Fatal(err)
					}
					out := enc.EncodeAll(in, nil)
					if stream {
						enc.Write(in)
						if err := enc.Close(); err != nil {
							t.Fatal(err)
						}
						out = buf.Bytes()
					}
					var h Header
					if err := h.Decode(out); err != nil || !h.FirstBlock.OK {
						t.Fatal("no first block:", err)
					}
					// Compressed, the block is 1 byte too large, so it must be raw.
					// It also fails if the tuning no longer matches the encoder.
					if h.FirstBlock.Compressed {
						t.Errorf("block of %d bytes is compressed to %d bytes, want raw", n, h.FirstBlock.CompressedSize)
					}
					got, err := dec.DecodeAll(out, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, in) {
						t.Fatal("decoded does not match")
					}
				})
			}
		}
	}
}

// huffTunedLits returns n bytes that huff0 codes to n-5 bytes in 4 streams.
// No 3 bytes repeat, so no encoder finds a match.
func huffTunedLits(t *testing.T, n int, optimalDepth bool) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(int64(n)))
	b := make([]byte, n)
	seen := make([]bool, 1<<24)
	key := func(i int) int { return int(b[i])<<16 | int(b[i+1])<<8 | int(b[i+2]) }
	// Symbols 0 and 1 are rare, so more of them make the output smaller.
	sym := func(rare bool) byte {
		if rare {
			return byte(rng.Intn(2))
		}
		return byte(2 + rng.Intn(254))
	}
	for i := range b {
		for b[i] = sym(rng.Intn(100) < 4); i >= 2 && seen[key(i-2)]; {
			b[i] = sym(rng.Intn(100) < 4)
		}
		if i >= 2 {
			seen[key(i-2)] = true
		}
	}
	// mark marks the 3 bytes from each of b[lo:hi+1], or fails if one repeats.
	mark := func(lo, hi int) bool {
		for j := lo; j <= hi; j++ {
			if seen[key(j)] {
				for k := lo; k < j; k++ {
					seen[key(k)] = false
				}
				return false
			}
			seen[key(j)] = true
		}
		return true
	}
	for range 1000 {
		diff := 64
		out, _, err := huff0.Compress4X(b, &huff0.Scratch{OptimalDepth: optimalDepth})
		if err == nil {
			diff = len(out) - (n - 5)
		}
		if diff == 0 {
			return b
		}
		for range 4 * max(diff, -diff) {
			i := rng.Intn(n)
			for (b[i] < 2) == (diff > 0) {
				i = (i + 1) % n
			}
			lo, hi := max(i-2, 0), min(i, n-3)
			for j := lo; j <= hi; j++ {
				seen[key(j)] = false
			}
			old := b[i]
			if b[i] = sym(diff > 0); !mark(lo, hi) {
				b[i] = old
				mark(lo, hi)
			}
		}
	}
	t.Fatalf("found no %d bytes that huff0 codes to %d bytes", n, n-5)
	return nil
}

// TestEncoderKeepLiteralTable checks that best and ultra keep the decoder's
// literal table after a block whose new table is not written. In "lits", the
// second block builds a table but stores its literals raw, and the third
// reuses the table of the first. In "block", ultra builds a table for the
// first block but stores the block raw, so the second needs a table of its own.
func TestEncoderKeepLiteralTable(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	in := make([]byte, 0, 3<<10)
	for range 1 << 10 {
		in = append(in, 'a'+byte(rng.Intn(16)))
	}
	// No 3 bytes repeat in the second block, so it is all literals.
	seen := map[[3]byte]bool{}
	for i := 2; i < len(in); i++ {
		seen[[3]byte(in[i-2:i+1])] = true
	}
	for len(in) < 2<<10 {
		c := byte(rng.Intn(256))
		if rng.Float64() < 0.53 {
			c = byte(rng.Intn(64))
		}
		if g := [3]byte{in[len(in)-2], in[len(in)-1], c}; !seen[g] {
			seen[g] = true
			in = append(in, c)
		}
	}
	for range 1 << 10 {
		in = append(in, 'a'+byte(rng.Intn(16)))
	}
	// Best and ultra differ in OptimalDepth.
	for _, depth := range []bool{false, true} {
		if out, _, err := huff0.Compress4X(in[1<<10:2<<10], &huff0.Scratch{OptimalDepth: depth}); err != nil || len(out) >= 1<<10 {
			t.Fatal("the second block no longer gets a new literal table:", len(out), err)
		}
	}
	// Ultra codes the first block with a new literal table and a short match,
	// which together are larger than the raw block.
	rng = rand.New(rand.NewSource(0))
	var block []byte
	for len(block) < 1000 {
		c := byte(rng.Intn(256))
		if rng.Intn(100) < 22 {
			c = 'a' + byte(rng.Intn(16))
		}
		block = append(block, c)
	}
	for range 7 {
		block = append(block, block[len(block)-500])
	}
	block = appendRandom(block, rng, 1<<10-len(block))
	for range 1 << 10 {
		block = append(block, block[rng.Intn(1<<10)])
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for _, tc := range []struct {
		name   string
		in     []byte
		levels []EncoderLevel
		want   []int
	}{
		{"lits", in, []EncoderLevel{SpeedBestCompression, SpeedUltraCompression}, []int{int(literalsBlockCompressed), -1, int(literalsBlockTreeless)}},
		{"block", block, []EncoderLevel{SpeedUltraCompression}, []int{-1, int(literalsBlockCompressed)}},
	} {
		for _, level := range tc.levels {
			// 0 is EncodeAll.
			for _, conc := range []int{0, 1, 2} {
				t.Run(fmt.Sprint(tc.name, "-", level, "-", conc), func(t *testing.T) {
					var buf bytes.Buffer
					enc, err := NewWriter(&buf, WithEncoderLevel(level), WithWindowSize(1<<10), WithEncoderConcurrency(max(conc, 1)))
					if err != nil {
						t.Fatal(err)
					}
					out := enc.EncodeAll(tc.in, nil)
					if conc > 0 {
						enc.Write(tc.in)
						if err := enc.Close(); err != nil {
							t.Fatal(err)
						}
						out = buf.Bytes()
					}
					got, err := dec.DecodeAll(out, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, tc.in) {
						t.Fatal("decoded does not match")
					}
					if lt := literalTypes(t, out); len(lt) < len(tc.want) || !slices.Equal(lt[:len(tc.want)], tc.want) {
						t.Fatalf("literal types %v, want %v first", lt, tc.want)
					}
				})
			}
		}
	}
}

// literalTypes returns the literals section type of each block in the frame
// comp, or -1 for a raw block and -2 for an RLE block.
func literalTypes(t *testing.T, comp []byte) []int {
	t.Helper()
	types, _ := blockTypes(t, comp, false)
	return types
}

// blockTypes returns literalTypes and, with seqs, whether each block has
// sequences. Finding them decodes the literals, so the frame must not use a dictionary.
func blockTypes(t *testing.T, comp []byte, seqs bool) ([]int, []bool) {
	t.Helper()
	dec, err := NewReader(nil, WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	block, _ := dec.decoders.Get()
	defer dec.decoders.Put(block)
	frame := block.localFrame
	frame.bBuf = comp
	frame.history.reset()
	if err := frame.reset(&frame.bBuf); err != nil {
		t.Fatal(err)
	}
	var types []int
	var hasSeqs []bool
	for {
		if err := block.reset(frame.rawInput, frame.WindowSize); err != nil {
			t.Fatal(err)
		}
		lt := -1
		switch block.Type {
		case blockTypeCompressed:
			lt = int(block.data[0] & 3)
		case blockTypeRLE:
			lt = -2
		}
		types = append(types, lt)
		if seqs {
			var rem []byte
			if block.Type == blockTypeCompressed {
				// Every block, since treeless literals use an earlier table.
				if rem, err = block.decodeLiterals(block.data, &frame.history); err != nil {
					t.Fatal(err)
				}
			}
			// The sequences section starts with 0 if there are none.
			hasSeqs = append(hasSeqs, len(rem) > 0 && rem[0] != 0)
		}
		if block.Last {
			return types, hasSeqs
		}
	}
}

// splitTestBlock builds a block from sequences, and where to cut it.
type splitTestBlock struct {
	src, lits []byte
	seqs      []seq
	cuts      []int
	rep       [3]uint32 // repeat offsets of the parse
}

// add appends lits and a match of n bytes at offset off, coded as offBase:
// off+3 or a repeat code.
func (b *splitTestBlock) add(t *testing.T, lits []byte, off uint32, n int, offBase uint32) {
	t.Helper()
	ll0 := len(lits) == 0
	if offBase <= 3 && repOffset(b.rep, offBase, ll0) != off || offBase > 3 && offBase != off+3 {
		t.Fatalf("offBase %d is not offset %d", offBase, off)
	}
	b.rep = ultraNewRep(b.rep, offBase, ll0)
	b.addLits(lits)
	for range n {
		b.src = append(b.src, b.src[len(b.src)-int(off)])
	}
	b.seqs = append(b.seqs, seq{litLen: uint32(len(lits)), matchLen: uint32(n - zstdMinMatch), offset: offBase})
}

// addLits appends literals after the last sequence.
func (b *splitTestBlock) addLits(lits []byte) {
	b.src = append(b.src, lits...)
	b.lits = append(b.lits, lits...)
}

// cut starts a part.
func (b *splitTestBlock) cut() {
	b.cuts = append(b.cuts, len(b.seqs))
}

// encode encodes the block with blk, cut into parts if split is set.
func (b *splitTestBlock) encode(t *testing.T, blk *blockEnc, split, last bool) []byte {
	t.Helper()
	blk.reset(nil)
	blk.literals = append(blk.literals, b.lits...)
	blk.sequences = append(blk.sequences, b.seqs...)
	blk.size, blk.last = len(b.src), last
	blk.postSplit, blk.testCuts = split, nil
	if split {
		blk.testCuts = b.cuts
	}
	if err := blk.encode(b.src, false, true); err != nil {
		t.Fatal(err)
	}
	return blk.output
}

// TestEncoderSplitParts writes a block as compressed, raw and RLE parts. The
// decoder only gets the repeat offsets of compressed parts, so repeat codes
// after the others are written as offsets.
func TestEncoderSplitParts(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	text := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a' + byte(rng.Intn(16))
		}
		return b
	}
	b := splitTestBlock{rep: [3]uint32{1, 4, 8}}
	b.add(t, text(400), 100, 20, 103)
	for i := range 40 {
		b.add(t, text(20), 100, 8+i%5, 1)
	}
	b.cut()
	// Raw.
	b.add(t, appendRandom(nil, rng, 3000), 1234, 4, 1237)
	b.cut()
	// The decoder has neither offset 1 from the block start nor 1234
	// as these repeat codes.
	b.add(t, text(10), 1, 6, 3)
	b.add(t, nil, 1234, 6, 1)
	for range 40 {
		b.add(t, text(20), 1234, 8, 1)
	}
	b.cut()
	// RLE.
	b.add(t, []byte{'z'}, 1, 1000, 4)
	b.cut()
	// The decoder does not have offset 1.
	b.add(t, text(10), 1, 5, 1)
	b.add(t, text(20), 1234, 8, 2)
	for range 40 {
		b.add(t, text(20), 1234, 8, 1)
	}
	b.addLits(text(30))

	var blk blockEnc
	blk.init()
	blk.initNewEncode()
	blk.highRatio, blk.ultra = true, true
	// Encoders leave the repeat offsets after the parse.
	blk.startReps, blk.recentOffsets = [3]uint32{1, 4, 8}, b.rep
	comp := frameHeader{ContentSize: uint64(len(b.src)), WindowSize: 1 << 20}.appendTo(nil)
	comp = append(comp, b.encode(t, &blk, true, true)...)
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(comp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, b.src) {
		t.Fatal("decoded does not match")
	}
	if lt := literalTypes(t, comp); len(lt) != 5 || lt[0] < 0 || lt[1] != -1 || lt[2] < 0 || lt[3] != -2 || lt[4] < 0 {
		t.Fatalf("literal types %v, want parts compressed, raw, compressed, RLE, compressed", lt)
	}
	if n := len(blk.split.offsets); n != 3 {
		t.Fatalf("%d repeat codes written as offsets, want 3", n)
	}
}

// TestEncoderSplitBound writes a block whose parts would be larger than a raw
// block, so it is written whole. Neither it nor the next block may use the
// tables of the parts.
func TestEncoderSplitBound(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	text := func(n int, from byte) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = from + byte(rng.Intn(16))
		}
		return b
	}
	// Each block gets new tables.
	tables := func(t *testing.T, rep [3]uint32, from byte, ml int) *splitTestBlock {
		b := &splitTestBlock{rep: rep}
		b.add(t, text(200, from), 50, ml, 53)
		for i := range 60 {
			b.add(t, text(5, from), 50, ml+i%3, 1)
		}
		return b
	}
	// newFrame: the block starts a frame, after one with other tables.
	// rewrite: some parts write repeat codes as offsets.
	for _, tc := range []struct{ newFrame, rewrite bool }{{false, false}, {true, false}, {false, true}} {
		newFrame := tc.newFrame
		t.Run(fmt.Sprintf("%+v", tc), func(t *testing.T) {
			var blk blockEnc
			blk.init()
			blk.initNewEncode()
			blk.highRatio, blk.ultra = true, true
			blk.startReps = [3]uint32{1, 4, 8}
			a := tables(t, blk.startReps, 'a', 12)
			comp := append([]byte{}, a.encode(t, &blk, false, false)...)
			// c is like the first part of b, or like a in a new frame.
			c := tables(t, [3]uint32{}, 'Q', 4)
			if newFrame {
				blk.initNewEncode()
				comp = comp[:0]
				c, a.src = tables(t, [3]uint32{}, 'a', 12), nil
			}
			blk.startReps = [3]uint32{}
			b := tables(t, blk.startReps, 'Q', 4)
			// Raw parts, 3 bytes larger each. As one block, their sequences
			// fit the tables of the first part, and their literals no table.
			for i := range 400 {
				b.cut()
				b.add(t, appendRandom(nil, rng, 20), 50, 4+i%3, 1)
				if tc.rewrite && i%10 == 0 {
					// The next part writes its repeat code of 40 as an offset,
					// since the decoder lacks 40. As one block, the repeat code
					// after it would then not refer to 50.
					b.add(t, appendRandom(nil, rng, 20), 40, 4, 43)
					b.cut()
					b.add(t, appendRandom(nil, rng, 20), 40, 4, 1)
					b.add(t, nil, 50, 4, 1)
				}
			}
			out := b.encode(t, &blk, true, false)
			if len(out) > len(b.src)+3 {
				t.Fatalf("%d bytes block is %d bytes", len(b.src), len(out))
			}
			comp = append(comp, out...)
			comp = append(comp, c.encode(t, &blk, false, true)...)
			src := slices.Concat(a.src, b.src, c.src)
			comp = append(frameHeader{ContentSize: uint64(len(src)), WindowSize: 1 << 20}.appendTo(nil), comp...)
			dec, err := NewReader(nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dec.Close()
			got, err := dec.DecodeAll(comp, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, src) {
				t.Fatal("decoded does not match")
			}
			// b is one block.
			want := 3
			if newFrame {
				want = 2
			}
			if n := len(literalTypes(t, comp)); n != want {
				t.Fatalf("%d blocks, want %d", n, want)
			}
		})
	}
}

// splitSpy turns on the post-block splitter for every block of an encoder.
// With every > 0, it cuts blocks into parts of that many sequences instead.
type splitSpy struct {
	encoder
	every int
}

func (s *splitSpy) Encode(blk *blockEnc, src []byte) {
	s.encoder.Encode(blk, src)
	s.cut(blk)
}

func (s *splitSpy) EncodeNoHist(blk *blockEnc, src []byte) {
	s.encoder.EncodeNoHist(blk, src)
	s.cut(blk)
}

func (s *splitSpy) cut(blk *blockEnc) {
	blk.postSplit, blk.testCuts = true, nil
	if s.every <= 0 {
		return
	}
	blk.testCuts = []int{}
	for c := s.every; c < len(blk.sequences); c += s.every {
		blk.testCuts = append(blk.testCuts, c)
	}
}

// setSplitSpy makes the encoders of e split blocks, and cut them every
// every sequences. Streams must call it after each Reset.
func setSplitSpy(e *Encoder, every int) {
	e.init.Do(e.initialize)
	spy := func(enc encoder) encoder {
		if s, ok := enc.(*splitSpy); ok {
			s.every = every
			return s
		}
		return &splitSpy{encoder: enc, every: every}
	}
	forEachEncoder(e, spy)
	if e.state.encoder != nil {
		e.state.encoder = spy(e.state.encoder)
	}
}

// TestEncoderPostSplit splits blocks on every path. Best does not split by
// itself. Its parse uses no repeat codes before the third sequence of a
// block, so its parts can start with unknown repeat offsets.
func TestEncoderPostSplit(t *testing.T) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	// Two jobs with concurrent blocks.
	in = in[:768<<10]
	dict := readDicts(t, testCreateZipReader("testdata/dict-tests-small.zip", t))[0]
	for _, level := range []EncoderLevel{SpeedBestCompression, SpeedUltraCompression} {
		for _, path := range []string{"encodeall", "sync", "async", "jobs", "dict"} {
			t.Run(fmt.Sprint(level, "-", path), func(t *testing.T) {
				opts := []EOption{WithEncoderLevel(level), WithEncoderConcurrency(2)}
				switch path {
				case "sync":
					opts = append(opts, WithEncoderConcurrency(1))
				case "jobs":
					// Jobs of 512 KiB.
					opts = append(opts, WithConcurrentBlocks(true), WithWindowSize(128<<10))
				case "dict":
					opts = append(opts, WithEncoderDict(dict))
				}
				e, err := NewWriter(nil, opts...)
				if err != nil {
					t.Fatal(err)
				}
				var buf bytes.Buffer
				e.Reset(&buf)
				if level == SpeedBestCompression {
					setSplitSpy(e, 0)
				}
				var comp []byte
				if path == "encodeall" || path == "dict" {
					comp = e.EncodeAll(in, nil)
				} else {
					if _, err := e.Write(in); err != nil {
						t.Fatal(err)
					}
					if err := e.Close(); err != nil {
						t.Fatal(err)
					}
					comp = buf.Bytes()
					if path == "jobs" && e.state.jobs.jobSeq != 2 {
						t.Fatalf("%d jobs, want 2", e.state.jobs.jobSeq)
					}
				}
				dec, err := NewReader(nil, WithDecoderDicts(dict))
				if err != nil {
					t.Fatal(err)
				}
				defer dec.Close()
				got, err := dec.DecodeAll(comp, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, in) {
					t.Fatal("decoded does not match")
				}
				// Six blocks and maybe an empty last one, unless split.
				if n := len(literalTypes(t, comp)); n <= 7 {
					t.Fatalf("%d blocks; no block was split", n)
				}
			})
		}
	}
}

func TestEncoder_EncodeAllPi(t *testing.T) {
	in, err := os.ReadFile("../testdata/pi.txt")
	if err != nil {
		t.Fatal(err)
	}
	testWindowSizes := testWindowSizes
	if testing.Short() {
		testWindowSizes = []int{1 << 20}
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for level := speedNotSet + 1; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			if isRaceTest && level >= SpeedBestCompression {
				t.SkipNow()
			}
			for _, windowSize := range testWindowSizes {
				t.Run(fmt.Sprintf("window:%d", windowSize), func(t *testing.T) {
					e, err := NewWriter(nil, WithEncoderLevel(level), WithWindowSize(windowSize))
					if err != nil {
						t.Fatal(err)
					}
					defer e.Close()
					start := time.Now()
					dst := e.EncodeAll(in, nil)
					t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
					mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
					t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

					decoded, err := dec.DecodeAll(dst, nil)
					if err != nil {
						t.Error(err, len(decoded))
					}
					if !bytes.Equal(decoded, in) {
						os.WriteFile("testdata/"+t.Name()+"-pi.txt.got", decoded, os.ModePerm)
						t.Fatal("Decoded does not match")
					}
					t.Log("Encoded content matched")
				})
			}
		})
	}
}

func TestWithEncoderPadding(t *testing.T) {
	n := 100
	if testing.Short() {
		n = 2
	}
	rng := rand.New(rand.NewSource(0x1337))
	d, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	for i := 0; i < n; i++ {
		padding := (rng.Int() & 0xfff) + 1
		src := make([]byte, (rng.Int()&0xfffff)+1)
		for i := range src {
			src[i] = uint8(rng.Uint32()) & 7
		}
		e, err := NewWriter(nil, WithEncoderPadding(padding), WithEncoderCRC(rng.Uint32()&1 == 0))
		if err != nil {
			t.Fatal(err)
		}
		// Test the added padding is invisible.
		dst := e.EncodeAll(src, nil)
		if len(dst)%padding != 0 {
			t.Fatalf("wanted size to be multiple of %d, got size %d with remainder %d", padding, len(dst), len(dst)%padding)
		}
		got, err := d.DecodeAll(dst, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, got) {
			t.Fatal("output mismatch")
		}
		// Test when we supply data as well.
		dst = e.EncodeAll(src, make([]byte, rng.Int()&255))
		if len(dst)%padding != 0 {
			t.Fatalf("wanted size to be multiple of %d, got size %d with remainder %d", padding, len(dst), len(dst)%padding)
		}

		// Test using the writer.
		var buf bytes.Buffer
		e.ResetContentSize(&buf, int64(len(src)))
		_, err = io.Copy(e, bytes.NewBuffer(src))
		if err != nil {
			t.Fatal(err)
		}
		err = e.Close()
		if err != nil {
			t.Fatal(err)
		}
		dst = buf.Bytes()
		if len(dst)%padding != 0 {
			t.Fatalf("wanted size to be multiple of %d, got size %d with remainder %d", padding, len(dst), len(dst)%padding)
		}
		// Test the added padding is invisible.
		got, err = d.DecodeAll(dst, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, got) {
			t.Fatal("output mismatch")
		}
		// Try after reset
		buf.Reset()
		e.Reset(&buf)
		_, err = io.Copy(e, bytes.NewBuffer(src))
		if err != nil {
			t.Fatal(err)
		}
		err = e.Close()
		if err != nil {
			t.Fatal(err)
		}
		dst = buf.Bytes()
		if len(dst)%padding != 0 {
			t.Fatalf("wanted size to be multiple of %d, got size %d with remainder %d", padding, len(dst), len(dst)%padding)
		}
		// Test the added padding is invisible.
		got, err = d.DecodeAll(dst, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(src, got) {
			t.Fatal("output mismatch")
		}
	}
}
func TestEncoder_EncoderXML(t *testing.T) {
	testEncoderRoundtrip(t, "./testdata/xml.zst", []byte{0x56, 0x54, 0x69, 0x8e, 0x40, 0x50, 0x11, 0xe})
	testEncoderRoundtripWriter(t, "./testdata/xml.zst", []byte{0x56, 0x54, 0x69, 0x8e, 0x40, 0x50, 0x11, 0xe})
}

func TestEncoder_EncoderTwain(t *testing.T) {
	testEncoderRoundtrip(t, "../testdata/Mark.Twain-Tom.Sawyer.txt", []byte{0x12, 0x1f, 0x12, 0x70, 0x79, 0x37, 0x1f, 0xc6})
	testEncoderRoundtripWriter(t, "../testdata/Mark.Twain-Tom.Sawyer.txt", []byte{0x12, 0x1f, 0x12, 0x70, 0x79, 0x37, 0x1f, 0xc6})
}

func TestEncoder_EncoderPi(t *testing.T) {
	testEncoderRoundtrip(t, "../testdata/pi.txt", []byte{0xe7, 0xe5, 0x25, 0x39, 0x92, 0xc7, 0x4a, 0xfb})
	testEncoderRoundtripWriter(t, "../testdata/pi.txt", []byte{0xe7, 0xe5, 0x25, 0x39, 0x92, 0xc7, 0x4a, 0xfb})
}

func TestEncoder_EncoderSilesia(t *testing.T) {
	testEncoderRoundtrip(t, "testdata/silesia.tar", []byte{0xa5, 0x5b, 0x5e, 0xe, 0x5e, 0xea, 0x51, 0x6b})
	testEncoderRoundtripWriter(t, "testdata/silesia.tar", []byte{0xa5, 0x5b, 0x5e, 0xe, 0x5e, 0xea, 0x51, 0x6b})
}

func TestEncoder_EncoderSimple(t *testing.T) {
	testEncoderRoundtrip(t, "testdata/z000028", []byte{0x8b, 0x2, 0x37, 0x70, 0x92, 0xb, 0x98, 0x95})
	testEncoderRoundtripWriter(t, "testdata/z000028", []byte{0x8b, 0x2, 0x37, 0x70, 0x92, 0xb, 0x98, 0x95})
}

func TestEncoder_EncoderHTML(t *testing.T) {
	testEncoderRoundtrip(t, "../testdata/html.txt", []byte{0x35, 0xa9, 0x5c, 0x37, 0x20, 0x9e, 0xc3, 0x37})
	testEncoderRoundtripWriter(t, "../testdata/html.txt", []byte{0x35, 0xa9, 0x5c, 0x37, 0x20, 0x9e, 0xc3, 0x37})
}

func TestEncoder_EncoderEnwik9(t *testing.T) {
	//testEncoderRoundtrip(t, "./testdata/enwik9.zst", []byte{0x28, 0xfa, 0xf4, 0x30, 0xca, 0x4b, 0x64, 0x12})
	//testEncoderRoundtripWriter(t, "./testdata/enwik9.zst", []byte{0x28, 0xfa, 0xf4, 0x30, 0xca, 0x4b, 0x64, 0x12})
}

// test roundtrip using io.ReaderFrom interface.
func testEncoderRoundtrip(t *testing.T, file string, wantCRC []byte) {
	for _, opt := range getEncOpts(1) {
		t.Run(opt.name, func(t *testing.T) {
			opt := opt
			//t.Parallel()
			f, err := os.Open(file)
			if err != nil {
				if os.IsNotExist(err) {
					t.Skip("No input file:", file)
					return
				}
				t.Fatal(err)
			}
			defer f.Close()
			if stat, err := f.Stat(); testing.Short() && err == nil {
				if stat.Size() > 10000 {
					t.SkipNow()
				}
			}
			input := io.Reader(f)
			if strings.HasSuffix(file, ".zst") {
				dec, err := NewReader(f)
				if err != nil {
					t.Fatal(err)
				}
				input = dec
				defer dec.Close()
			}
			wantCRC := wantCRC
			if opt.level == SpeedUltraCompression {
				// The slow ultra level only encodes a prefix.
				b, err := io.ReadAll(io.LimitReader(input, 512<<10))
				if err != nil {
					t.Fatal(err)
				}
				input = bytes.NewReader(b)
				d := xxhash.New()
				d.Write(b)
				wantCRC = d.Sum(nil)
			}

			pr, pw := io.Pipe()
			dec2, err := NewReader(pr)
			if err != nil {
				t.Fatal(err)
			}
			defer dec2.Close()

			enc, err := NewWriter(pw, opt.o...)
			if err != nil {
				t.Fatal(err)
			}
			defer enc.Close()
			var wantSize int64
			start := time.Now()
			go func() {
				n, err := enc.ReadFrom(input)
				if err != nil {
					t.Error(err)
					return
				}
				wantSize = n
				err = enc.Close()
				if err != nil {
					t.Error(err)
					return
				}
				pw.Close()
			}()
			var gotSize int64

			// Check CRC
			d := xxhash.New()
			if true {
				gotSize, err = io.Copy(d, dec2)
			} else {
				fout, err := os.Create(file + ".got")
				if err != nil {
					t.Fatal(err)
				}
				gotSize, err = io.Copy(io.MultiWriter(fout, d), dec2)
				if err != nil {
					t.Fatal(err)
				}
			}
			if wantSize != gotSize {
				t.Errorf("want size (%d) != got size (%d)", wantSize, gotSize)
			}
			if err != nil {
				t.Fatal(err)
			}
			if gotCRC := d.Sum(nil); len(wantCRC) > 0 && !bytes.Equal(gotCRC, wantCRC) {
				t.Errorf("crc mismatch %#v (want) != %#v (got)", wantCRC, gotCRC)
			} else if len(wantCRC) != 8 {
				t.Logf("Unable to verify CRC: %#v", gotCRC)
			} else {
				t.Logf("CRC Verified: %#v", gotCRC)
			}
			t.Log("Encoder len", wantSize)
			mbpersec := (float64(wantSize) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
			t.Logf("Encoded+Decoded %d bytes with %.2f MB/s", wantSize, mbpersec)
		})
	}
}

type writerWrapper struct {
	w io.Writer
}

func (w writerWrapper) Write(p []byte) (n int, err error) {
	return w.w.Write(p)
}

// test roundtrip using plain io.Writer interface.
func testEncoderRoundtripWriter(t *testing.T, file string, wantCRC []byte) {
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("No input file:", file)
			return
		}
		t.Fatal(err)
	}
	defer f.Close()
	if stat, err := f.Stat(); testing.Short() && err == nil {
		if stat.Size() > 10000 {
			t.SkipNow()
		}
	}
	input := io.Reader(f)
	if strings.HasSuffix(file, ".zst") {
		dec, err := NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		input = dec
		defer dec.Close()
	}

	pr, pw := io.Pipe()
	dec2, err := NewReader(pr)
	if err != nil {
		t.Fatal(err)
	}
	defer dec2.Close()

	enc, err := NewWriter(pw, WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	encW := writerWrapper{w: enc}
	var wantSize int64
	start := time.Now()
	go func() {
		n, err := io.CopyBuffer(encW, input, make([]byte, 1337))
		if err != nil {
			t.Error(err)
			return
		}
		wantSize = n
		err = enc.Close()
		if err != nil {
			t.Error(err)
			return
		}
		pw.Close()
	}()
	var gotSize int64

	// Check CRC
	d := xxhash.New()
	if true {
		gotSize, err = io.Copy(d, dec2)
	} else {
		fout, err := os.Create(file + ".got")
		if err != nil {
			t.Fatal(err)
		}
		gotSize, err = io.Copy(io.MultiWriter(fout, d), dec2)
		if err != nil {
			t.Fatal(err)
		}
	}
	if wantSize != gotSize {
		t.Errorf("want size (%d) != got size (%d)", wantSize, gotSize)
	}
	if err != nil {
		t.Fatal(err)
	}
	if gotCRC := d.Sum(nil); len(wantCRC) > 0 && !bytes.Equal(gotCRC, wantCRC) {
		t.Errorf("crc mismatch %#v (want) != %#v (got)", wantCRC, gotCRC)
	} else if len(wantCRC) != 8 {
		t.Logf("Unable to verify CRC: %#v", gotCRC)
	} else {
		t.Logf("CRC Verified: %#v", gotCRC)
	}
	t.Log("Fast Encoder len", wantSize)
	mbpersec := (float64(wantSize) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded+Decoded %d bytes with %.2f MB/s", wantSize, mbpersec)
}

func TestEncoder_EncodeAllSilesia(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	in, err := os.ReadFile("testdata/silesia.tar")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("Missing testdata/silesia.tar")
			return
		}
		t.Fatal(err)
	}

	var e Encoder
	start := time.Now()
	dst := e.EncodeAll(in, nil)
	t.Log("Fast Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

	dec, err := NewReader(nil, WithDecoderMaxMemory(220<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		os.WriteFile("testdata/"+t.Name()+"-silesia.tar.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

// TestSilesiaRoundTrip round-trips testdata/silesia.tar at each level through
// the streaming Writer and Reader and through EncodeAll and DecodeAll.
func TestSilesiaRoundTrip(t *testing.T) {
	in := silesiatest.Tar(t)
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	for level := SpeedFastest; level < speedLast; level++ {
		t.Run(level.String(), func(t *testing.T) {
			in := in
			if level == SpeedUltraCompression {
				in = in[:min(len(in), 32<<20)]
			}
			var buf bytes.Buffer
			w, err := NewWriter(&buf, WithEncoderLevel(level))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.Copy(w, bytes.NewReader(in)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := NewReader(&buf)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal("Reader:", err)
			}
			if !bytes.Equal(got, in) {
				t.Fatal("Writer/Reader: output differs from input")
			}

			enc, err := NewWriter(nil, WithEncoderLevel(level))
			if err != nil {
				t.Fatal(err)
			}
			comp := enc.EncodeAll(in, nil)
			enc.Close()
			got, err = dec.DecodeAll(comp, got[:0])
			if err != nil {
				t.Fatal("DecodeAll:", err)
			}
			if !bytes.Equal(got, in) {
				t.Fatal("EncodeAll/DecodeAll: output differs from input")
			}
		})
	}
}

func TestEncoderReadFrom(t *testing.T) {
	buffer := bytes.NewBuffer(nil)
	encoder, err := NewWriter(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.ReadFrom(strings.NewReader("0")); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}

	dec, _ := NewReader(nil)
	toDec := buffer.Bytes()
	toDec = append(toDec, toDec...)
	decoded, err := dec.DecodeAll(toDec, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal([]byte("00"), decoded) {
		t.Logf("encoded: % x\n", buffer.Bytes())
		t.Fatalf("output mismatch, got %s", string(decoded))
	}
	dec.Close()
}

func TestInterleavedWriteReadFrom(t *testing.T) {
	var encoded bytes.Buffer

	enc, err := NewWriter(&encoded)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := enc.Write([]byte("write1")); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write([]byte("write2")); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.ReadFrom(strings.NewReader("readfrom1")); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write([]byte("write3")); err != nil {
		t.Fatal(err)
	}

	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	dec, err := NewReader(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	gotb, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotb)

	if want := "write1write2readfrom1write3"; got != want {
		t.Errorf("got decoded %q, want %q", got, want)
	}
}

func TestEncoder_EncodeAllEmpty(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	var in []byte

	for _, opt := range getEncOpts(1) {
		t.Run(opt.name, func(t *testing.T) {
			e, err := NewWriter(nil, opt.o...)
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			dst := e.EncodeAll(in, nil)
			t.Log("Block Encoder len", len(in), "-> zstd len", len(dst), dst)

			dec, err := NewReader(nil, WithDecoderMaxMemory(220<<20))
			if err != nil {
				t.Fatal(err)
			}
			defer dec.Close()
			decoded, err := dec.DecodeAll(dst, nil)
			if err != nil {
				t.Error(err, len(decoded))
			}
			if !bytes.Equal(decoded, in) {
				t.Fatal("Decoded does not match")
			}

			// Test buffer writer.
			var buf bytes.Buffer
			e.Reset(&buf)
			err = e.Close()
			if err != nil {
				t.Fatal(err)
			}
			dst = buf.Bytes()
			t.Log("Buffer Encoder len", len(in), "-> zstd len", len(dst))

			decoded, err = dec.DecodeAll(dst, nil)
			if err != nil {
				t.Error(err, len(decoded))
			}
			if !bytes.Equal(decoded, in) {
				t.Fatal("Decoded does not match")
			}

			t.Log("Encoded content matched")
		})
	}
}

func TestEncoder_EncodeAllEnwik9(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/enwik9.zst"
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("To run extended tests, download http://mattmahoney.net/dc/enwik9.zip unzip it \n" +
				"compress it with 'zstd -15 -T0 enwik9' and place it in " + file)
		}
	}
	defer f.Close()

	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	e, err := NewWriter(nil)
	dst := e.EncodeAll(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)
	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		os.WriteFile("testdata/"+t.Name()+"-enwik9.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoder_EncoderStreamEnwik9(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/enwik9.zst"
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("To run extended tests, download http://mattmahoney.net/dc/enwik9.zip unzip it \n" +
				"compress it with 'zstd -15 -T0 enwik9' and place it in " + file)
		}
	}
	defer f.Close()

	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := io.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	var dst bytes.Buffer
	e, err := NewWriter(&dst)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(e, bytes.NewBuffer(in))
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	t.Log("Full Encoder len", len(in), "-> zstd len", dst.Len())
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)
	if false {
		decoded, err := dec.DecodeAll(dst.Bytes(), nil)
		if err != nil {
			t.Error(err, len(decoded))
		}
		if !bytes.Equal(decoded, in) {
			os.WriteFile("testdata/"+t.Name()+"-enwik9.got", decoded, os.ModePerm)
			t.Fatal("Decoded does not match")
		}
		t.Log("Encoded content matched")
	}
}

func BenchmarkEncoder_EncodeAllXML(b *testing.B) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()

	dec, err := NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	in, err := io.ReadAll(dec)
	if err != nil {
		b.Fatal(err)
	}
	dec.Close()

	enc, _ := NewWriter(nil, WithEncoderConcurrency(1))
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	//b.Log("Output size:", len(dst))
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllSimple(b *testing.B) {
	in, err := os.ReadFile("testdata/z000028")
	if err != nil {
		b.Fatal(err)
	}

	for level := speedNotSet + 1; level < speedLast; level++ {
		b.Run(level.String(), func(b *testing.B) {
			enc, err := NewWriter(nil, WithEncoderConcurrency(1), WithEncoderLevel(level))
			if err != nil {
				b.Fatal(err)
			}
			defer enc.Close()
			dst := enc.EncodeAll(in, nil)
			wantSize := len(dst)
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				dst := enc.EncodeAll(in, dst[:0])
				if len(dst) != wantSize {
					b.Fatal(len(dst), "!=", wantSize)
				}
			}
		})
	}
}

func BenchmarkEncoder_EncodeAllSimple4K(b *testing.B) {
	in, err := os.ReadFile("testdata/z000028")
	if err != nil {
		b.Fatal(err)
	}
	in = in[:4096]

	for level := speedNotSet + 1; level < speedLast; level++ {
		b.Run(level.String(), func(b *testing.B) {
			enc, err := NewWriter(nil, WithEncoderConcurrency(1), WithEncoderLevel(level))
			if err != nil {
				b.Fatal(err)
			}
			defer enc.Close()
			dst := enc.EncodeAll(in, nil)
			wantSize := len(dst)
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				dst := enc.EncodeAll(in, dst[:0])
				if len(dst) != wantSize {
					b.Fatal(len(dst), "!=", wantSize)
				}
			}
		})
	}
}

func BenchmarkEncoder_EncodeAllHTML(b *testing.B) {
	in, err := os.ReadFile("../testdata/html.txt")
	if err != nil {
		b.Fatal(err)
	}

	enc, _ := NewWriter(nil, WithEncoderConcurrency(1))
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllTwain(b *testing.B) {
	in, err := os.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt")
	if err != nil {
		b.Fatal(err)
	}

	enc, _ := NewWriter(nil, WithEncoderConcurrency(1))
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllPi(b *testing.B) {
	in, err := os.ReadFile("../testdata/pi.txt")
	if err != nil {
		b.Fatal(err)
	}

	enc, _ := NewWriter(nil, WithEncoderConcurrency(1))
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

// BenchmarkEncoderCorpus encodes the files in the directory named by
// ZSTD_CORPUS with EncodeAll on one goroutine at every level, CRC off.
// Files ending in .zst are skipped. The compression ratio is reported.
func BenchmarkEncoderCorpus(b *testing.B) {
	dir := os.Getenv("ZSTD_CORPUS")
	if dir == "" {
		b.Skip("ZSTD_CORPUS not set")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		b.Fatal(err)
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".zst") {
			continue
		}
		in, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			for level := speedNotSet + 1; level < speedLast; level++ {
				b.Run(level.String(), func(b *testing.B) {
					enc, err := NewWriter(nil, WithEncoderConcurrency(1), WithEncoderLevel(level), WithEncoderCRC(false))
					if err != nil {
						b.Fatal(err)
					}
					defer enc.Close()
					dst := enc.EncodeAll(in, nil)
					b.SetBytes(int64(len(in)))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						dst = enc.EncodeAll(in, dst[:0])
					}
					b.ReportMetric(float64(len(in))/float64(len(dst)), "ratio")
				})
			}
		})
	}
}

func BenchmarkRandom4KEncodeAllFastest(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 4<<10)
	for i := range data {
		data[i] = uint8(rng.Intn(256))
	}
	enc, _ := NewWriter(nil, WithEncoderLevel(SpeedFastest), WithEncoderConcurrency(1))
	defer enc.Close()
	dst := enc.EncodeAll(data, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(data, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkRandom10MBEncodeAllFastest(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 10<<20)
	rng.Read(data)
	enc, _ := NewWriter(nil, WithEncoderLevel(SpeedFastest), WithEncoderConcurrency(2))
	defer enc.Close()
	dst := enc.EncodeAll(data, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(data, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkRandom4KEncodeAllDefault(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 4<<10)
	rng.Read(data)
	enc, _ := NewWriter(nil, WithEncoderLevel(SpeedDefault), WithEncoderConcurrency(1))
	defer enc.Close()
	dst := enc.EncodeAll(data, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(data, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkRandomEncodeAllDefault(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 10<<20)
	rng.Read(data)
	enc, _ := NewWriter(nil, WithEncoderLevel(SpeedDefault), WithEncoderConcurrency(1))
	defer enc.Close()
	dst := enc.EncodeAll(data, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(data, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkRandom10MBEncoderFastest(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 10<<20)
	rng.Read(data)
	wantSize := int64(len(data))
	enc, _ := NewWriter(io.Discard, WithEncoderLevel(SpeedFastest))
	defer enc.Close()
	n, err := io.Copy(enc, bytes.NewBuffer(data))
	if err != nil {
		b.Fatal(err)
	}
	if n != wantSize {
		b.Fatal(n, "!=", wantSize)
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(wantSize)
	for i := 0; i < b.N; i++ {
		enc.Reset(io.Discard)
		n, err := io.Copy(enc, bytes.NewBuffer(data))
		if err != nil {
			b.Fatal(err)
		}
		if n != wantSize {
			b.Fatal(n, "!=", wantSize)
		}
	}
}

func BenchmarkRandomEncoderDefault(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 10<<20)
	rng.Read(data)
	wantSize := int64(len(data))
	enc, _ := NewWriter(io.Discard, WithEncoderLevel(SpeedDefault))
	defer enc.Close()
	n, err := io.Copy(enc, bytes.NewBuffer(data))
	if err != nil {
		b.Fatal(err)
	}
	if n != wantSize {
		b.Fatal(n, "!=", wantSize)
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(wantSize)
	for i := 0; i < b.N; i++ {
		enc.Reset(io.Discard)
		n, err := io.Copy(enc, bytes.NewBuffer(data))
		if err != nil {
			b.Fatal(err)
		}
		if n != wantSize {
			b.Fatal(n, "!=", wantSize)
		}
	}
}

func TestHashHelpersMatchHashLen(t *testing.T) {
	inputs := []uint64{0, 1, 0xff, 0xffffffffff, 1 << 40, 1<<64 - 1, 0x0123456789abcdef}
	rng := rand.New(rand.NewSource(1))
	for range 1000 {
		inputs = append(inputs, rng.Uint64())
	}
	primeL, primeS := hashPrimes8and5()
	tests := []struct {
		name string
		got  func(u uint64) uint32
		want func(u uint64) uint32
	}{
		{
			name: "better long",
			got:  func(u uint64) uint32 { return betterHashL(u, primeL) },
			want: func(u uint64) uint32 { return hashLen(u, betterLongTableBits, betterLongLen) },
		},
		{
			name: "better short",
			got:  func(u uint64) uint32 { return betterHashS(u, primeS) },
			want: func(u uint64) uint32 { return hashLen(u, betterShortTableBits, betterShortLen) },
		},
		{
			name: "dfast long",
			got:  func(u uint64) uint32 { return dFastHashL(u, primeL) },
			want: func(u uint64) uint32 { return hashLen(u, dFastLongTableBits, dFastLongLen) },
		},
		{
			name: "dfast short",
			got:  func(u uint64) uint32 { return dFastHashS(u, primeS) },
			want: func(u uint64) uint32 { return hashLen(u, dFastShortTableBits, dFastShortLen) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, u := range inputs {
				if got, want := tt.got(u), tt.want(u); got != want {
					t.Fatalf("u=%#x: got %#x, want %#x", u, got, want)
				}
			}
		})
	}
}

func TestEncodeTo(t *testing.T) {
	data, err := os.ReadFile("testdata/comp-crashers.zip")
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

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
			encoded := EncodeTo(make([]byte, 0, len(in)/2), in)
			got, err := DecodeTo(make([]byte, 0, len(in)/2), encoded)
			if err != nil {
				t.Logf("error: %v\nwant: %v\ngot:  %v", err, len(in), len(got))
				t.Fatal(err)
			}
			if !bytes.Equal(in, got) {
				t.Errorf("decode mismatch for %s: want %d, got %d", tt.Name, len(in), len(got))
				t.Logf("want: %x\ngot:  %x", in, got)
			}
		})
	}
}

func TestSynctestEncoderOutlivesBubble(t *testing.T) {
	in := synctestInput()
	var enc *Encoder
	var compIn []byte
	synctest.Test(t, func(t *testing.T) {
		var err error
		enc, err = NewWriter(nil, WithEncoderConcurrency(2))
		if err != nil {
			t.Fatal(err)
		}
		compIn = enc.EncodeAll(in, nil)
	})
	defer enc.Close()

	compOut := enc.EncodeAll(in, nil)
	if !bytes.Equal(compIn, compOut) {
		t.Fatal("EncodeAll gave other output outside the bubble")
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(compOut, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, in) {
		t.Fatal("round trip differs")
	}
}
