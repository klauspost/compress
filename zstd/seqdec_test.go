package zstd

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
)

func TestSequenceDecsAdjustOffset(t *testing.T) {
	type result struct {
		offset     int
		prevOffset [3]int
	}

	tc := []struct {
		offset     int
		litLen     int
		offsetB    uint8
		prevOffset [3]int

		res result
	}{{
		offset:     444,
		litLen:     0,
		offsetB:    42,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     444,
			prevOffset: [3]int{444, 111, 222},
		},
	}, {
		offset:     0,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     111,
			prevOffset: [3]int{111, 222, 333},
		},
	}, {
		offset:     -1,
		litLen:     0,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     111,
			prevOffset: [3]int{111, 222, 333},
		},
	}, {
		offset:     1,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     222,
			prevOffset: [3]int{222, 111, 333},
		},
	}, {
		offset:     2,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     333,
			prevOffset: [3]int{333, 111, 222},
		},
	}, {
		offset:     3,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{111, 222, 333},

		res: result{
			offset:     110, // s.prevOffset[0] - 1
			prevOffset: [3]int{110, 111, 222},
		},
	}, {
		offset:     3,
		litLen:     1,
		offsetB:    0,
		prevOffset: [3]int{1, 222, 333},

		res: result{
			offset:     1,
			prevOffset: [3]int{1, 1, 222},
		},
	},
	}

	for i := range tc {
		// given
		var sd sequenceDecs
		for j := range 3 {
			sd.prevOffset[j] = tc[i].prevOffset[j]
		}

		// when
		offset := sd.adjustOffset(tc[i].offset, tc[i].litLen, tc[i].offsetB)

		// then
		if offset != tc[i].res.offset {
			t.Logf("result:   %d", offset)
			t.Logf("expected: %d", tc[i].res.offset)
			t.Errorf("testcase #%d: wrong function result", i)
		}

		for j := range 3 {
			if sd.prevOffset[j] != tc[i].res.prevOffset[j] {
				t.Logf("result:   %v", sd.prevOffset)
				t.Logf("expected: %v", tc[i].res.prevOffset)
				t.Errorf("testcase #%d: sd.prevOffset got wrongly updated", i)
				break
			}
		}
	}
}

type testSequence struct {
	n, lits, win int
	prevOffsets  [3]int
}

func (s *testSequence) parse(fn string) (ok bool) {
	n, err := fmt.Sscanf(fn, "n-%d-lits-%d-prev-%d-%d-%d-win-%d.blk", &s.n, &s.lits, &s.prevOffsets[0], &s.prevOffsets[1], &s.prevOffsets[2], &s.win)
	ok = err == nil && n == 6
	if !ok {
		fmt.Println("Unable to parse:", err, n)
	}
	return ok
}

func readDecoders(tb testing.TB, buf *bytes.Buffer, ref testSequence) sequenceDecs {
	s := sequenceDecs{
		litLengths:   sequenceDec{fse: &fseDecoder{}},
		offsets:      sequenceDec{fse: &fseDecoder{}},
		matchLengths: sequenceDec{fse: &fseDecoder{}},
		prevOffset:   ref.prevOffsets,
		dict:         nil,
		literals:     make([]byte, ref.lits, ref.lits+compressedBlockOverAlloc),
		out:          nil,
		nSeqs:        ref.n,
		br:           nil,
		seqSize:      0,
		windowSize:   ref.win,
		maxBits:      0,
	}

	s.litLengths.fse.mustReadFrom(buf)
	s.matchLengths.fse.mustReadFrom(buf)
	s.offsets.fse.mustReadFrom(buf)

	s.maxBits = s.litLengths.fse.maxBits + s.offsets.fse.maxBits + s.matchLengths.fse.maxBits
	s.br = &bitReader{}
	return s
}

func Test_seqdec_decode_regression(t *testing.T) {
	zr := testCreateZipReader("testdata/decode-regression.zip", t)

	for _, tt := range zr.File {
		t.Run(tt.Name, func(t *testing.T) {
			f, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()

			// Note: make sure we create stream reader
			dec, err := NewReader(f, WithDecoderConcurrency(4))
			if err != nil {
				t.Error(err)
				return
			}

			var buf []byte
			_, err = io.ReadFull(dec, buf)
			if err != nil {
				t.Error(err)
				return
			}
		})
	}
}

func Test_seqdec_decoder(t *testing.T) {
	const writeWant = false
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	want := map[string][]seqVals{}
	var wantOffsets = map[string][3]int{}
	if !writeWant {
		zr := testCreateZipReader("testdata/seqs-want.zip", t)
		tb := t
		for _, tt := range zr.File {
			var ref testSequence
			if !ref.parse(tt.Name) {
				tb.Skip("unable to parse:", tt.Name)
			}
			o, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			r := csv.NewReader(o)
			recs, err := r.ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			for i, rec := range recs {
				if i == 0 {
					var o [3]int
					o[0], _ = strconv.Atoi(rec[0])
					o[1], _ = strconv.Atoi(rec[1])
					o[2], _ = strconv.Atoi(rec[2])
					wantOffsets[tt.Name] = o
					continue
				}
				s := seqVals{}
				s.mo, _ = strconv.Atoi(rec[0])
				s.ml, _ = strconv.Atoi(rec[1])
				s.ll, _ = strconv.Atoi(rec[2])
				want[tt.Name] = append(want[tt.Name], s)
			}
			o.Close()
		}
	}
	zr := testCreateZipReader("testdata/seqs.zip", t)
	tb := t
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)
		seqs := make([]seqVals, ref.n)

		t.Run(tt.Name, func(t *testing.T) {
			fatalIf := func(err error) {
				if err != nil {
					t.Fatal(err)
				}
			}
			fatalIf(s.br.init(buf.Bytes()))
			fatalIf(s.litLengths.init(s.br))
			fatalIf(s.offsets.init(s.br))
			fatalIf(s.matchLengths.init(s.br))

			err := s.decode(seqs)
			if err != nil {
				t.Error(err)
			}
			if writeWant {
				w, err := zw.Create(tt.Name)
				fatalIf(err)
				c := csv.NewWriter(w)
				w.Write(fmt.Appendf(nil, "%d,%d,%d\n", s.prevOffset[0], s.prevOffset[1], s.prevOffset[2]))
				for _, seq := range seqs {
					c.Write([]string{strconv.Itoa(seq.mo), strconv.Itoa(seq.ml), strconv.Itoa(seq.ll)})
				}
				c.Flush()
			} else {
				if s.prevOffset != wantOffsets[tt.Name] {
					t.Errorf("want offsets %v, got %v", wantOffsets[tt.Name], s.prevOffset)
				}

				if !reflect.DeepEqual(want[tt.Name], seqs) {
					t.Errorf("got %v\nwant %v", seqs, want[tt.Name])
				}
			}
		})
	}
	if writeWant {
		zw.Close()
		os.WriteFile("testdata/seqs-want.zip", buf.Bytes(), os.ModePerm)
	}
}

func Test_seqdec_execute(t *testing.T) {
	zr := testCreateZipReader("testdata/seqs.zip", t)
	tb := t
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)
		seqs := make([]seqVals, ref.n)

		fatalIf := func(err error) {
			if err != nil {
				tb.Fatal(err)
			}
		}
		fatalIf(s.br.init(buf.Bytes()))
		fatalIf(s.litLengths.init(s.br))
		fatalIf(s.offsets.init(s.br))
		fatalIf(s.matchLengths.init(s.br))

		fatalIf(s.decode(seqs))
		hist := make([]byte, ref.win)
		lits := s.literals

		t.Run(tt.Name, func(t *testing.T) {
			// Prefetch off, then forced on (which on these small windows
			// exercises the history-buffer redirect); output must match.
			var outs [2][]byte
			for i, on := range []bool{false, true} {
				restore := forceTwoPass(on)
				s.literals = lits
				s.out = nil
				err := s.execute(seqs, hist)
				restore()
				if err != nil {
					t.Fatal(err)
				}
				if len(s.out) != s.seqSize {
					t.Errorf("want %d != got %d", s.seqSize, len(s.out))
				}
				outs[i] = s.out
			}
			if !bytes.Equal(outs[0], outs[1]) {
				t.Error("output differs with the match prefetch enabled")
			}
		})
	}
}

func Test_seqdec_decodeSync(t *testing.T) {
	zr := testCreateZipReader("testdata/seqs.zip", t)
	tb := t
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)

		lits := s.literals
		hist := make([]byte, ref.win)
		t.Run(tt.Name, func(t *testing.T) {
			fatalIf := func(err error) {
				if err != nil {
					t.Fatal(err)
				}
			}
			// decodeSyncSimple picks the implementation from the output
			// buffer geometry: below a block of capacity it declines and the
			// pure-Go loop runs; at exactly a block the assembly runs with
			// bounds-exact copies; with compressedBlockOverAlloc slack on top
			// it runs with the extended 16-byte-block copies. Run all three
			// and require identical output, so the Go loop is the reference
			// for both assembly variants. The selector is asked directly, so
			// a geometry that does not select what the variant claims fails
			// here instead of silently running the Go loop three times. On
			// builds without the assembly all three take the Go loop.
			variants := []struct {
				name     string
				outCap   int
				wantSafe bool
			}{
				{"go", 0, true},
				{"asm-safe", maxCompressedBlockSize, true},
				{"asm-unsafe", maxCompressedBlockSizeAlloc, false},
			}
			var want []byte
			for i, v := range variants {
				fatalIf(s.br.init(buf.Bytes()))
				fatalIf(s.litLengths.init(s.br))
				fatalIf(s.offsets.init(s.br))
				fatalIf(s.matchLengths.init(s.br))
				s.literals = lits
				s.prevOffset = ref.prevOffsets
				s.out = make([]byte, 0, v.outCap)
				usesSafe := decodeSyncUsesSafe(&s)
				supported, err := s.decodeSyncSimple(hist)
				switch {
				case v.name == "go":
					if supported {
						t.Fatalf("%s: decodeSyncSimple accepted a buffer of capacity %d", v.name, v.outCap)
					}
					err = s.decodeSync(hist)
				case !supported && !haveSeqdecAsm:
					err = s.decodeSync(hist)
				case !supported:
					t.Fatalf("%s: decodeSyncSimple declined a buffer of capacity %d", v.name, v.outCap)
				case usesSafe != v.wantSafe:
					t.Fatalf("%s: safe copies = %v, want %v", v.name, usesSafe, v.wantSafe)
				}
				if err != nil {
					t.Fatalf("%s: %v", v.name, err)
				}
				if i == 0 {
					want = append([]byte(nil), s.out...)
				} else if !bytes.Equal(s.out, want) {
					t.Errorf("%s: output differs from the go path (%d vs %d bytes)", v.name, len(s.out), len(want))
				}
			}
		})
	}
}

func Benchmark_seqdec_decode(b *testing.B) {
	benchmark_seqdec_decode(b)
}

func benchmark_seqdec_decode(b *testing.B) {
	zr := testCreateZipReader("testdata/seqs.zip", b)
	tb := b
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)
		seqs := make([]seqVals, ref.n)

		b.Run(tt.Name, func(b *testing.B) {
			fatalIf := func(err error) {
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			t := time.Now()
			decoded := 0
			remain := uint(0)
			for i := 0; i < b.N; i++ {
				fatalIf(s.br.init(buf.Bytes()))
				fatalIf(s.litLengths.init(s.br))
				fatalIf(s.offsets.init(s.br))
				fatalIf(s.matchLengths.init(s.br))
				remain = s.br.remain()
				err := s.decode(seqs)
				if err != nil {
					b.Fatal(err)
				}
				decoded += ref.n
			}
			b.ReportMetric(float64(decoded)/time.Since(t).Seconds(), "seq/s")
			b.ReportMetric(float64(remain)/float64(s.nSeqs), "b/seq")
		})
	}
}

func Benchmark_seqdec_execute(b *testing.B) {
	zr := testCreateZipReader("testdata/seqs.zip", b)
	tb := b
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)
		seqs := make([]seqVals, ref.n)

		fatalIf := func(err error) {
			if err != nil {
				b.Fatal(err)
			}
		}
		fatalIf(s.br.init(buf.Bytes()))
		fatalIf(s.litLengths.init(s.br))
		fatalIf(s.offsets.init(s.br))
		fatalIf(s.matchLengths.init(s.br))

		fatalIf(s.decode(seqs))
		hist := make([]byte, ref.win)
		lits := s.literals

		b.Run(tt.Name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(s.seqSize))
			b.ResetTimer()
			t := time.Now()
			decoded := 0
			for i := 0; i < b.N; i++ {
				s.literals = lits
				if len(s.out) > 0 {
					s.out = s.out[:0]
				}
				fatalIf(s.execute(seqs, hist))
				decoded += ref.n
			}
			b.ReportMetric(float64(decoded)/time.Since(t).Seconds(), "seq/s")
		})
	}
}

func Benchmark_seqdec_decodeSync(b *testing.B) {
	zr := testCreateZipReader("testdata/seqs.zip", b)
	tb := b
	for _, tt := range zr.File {
		var ref testSequence
		if !ref.parse(tt.Name) {
			tb.Skip("unable to parse:", tt.Name)
		}
		r, err := tt.Open()
		if err != nil {
			tb.Error(err)
			return
		}

		seqData, err := io.ReadAll(r)
		if err != nil {
			tb.Error(err)
			return
		}
		var buf = bytes.NewBuffer(seqData)
		s := readDecoders(tb, buf, ref)

		lits := s.literals
		hist := make([]byte, ref.win)
		// Give the output the geometry the decoder sees in production, a
		// block of capacity plus compressedBlockOverAlloc slack, so
		// decodeSyncSimple takes the assembly path with the extended copies.
		// With no capacity it declines and the pure-Go loop is what gets
		// timed.
		s.out = make([]byte, 0, maxCompressedBlockSizeAlloc)
		b.Run(tt.Name, func(b *testing.B) {
			fatalIf := func(err error) {
				if err != nil {
					b.Fatal(err)
				}
			}
			decoded := 0
			b.ReportAllocs()
			b.ResetTimer()
			t := time.Now()

			for i := 0; i < b.N; i++ {
				fatalIf(s.br.init(buf.Bytes()))
				fatalIf(s.litLengths.init(s.br))
				fatalIf(s.offsets.init(s.br))
				fatalIf(s.matchLengths.init(s.br))
				s.literals = lits
				s.prevOffset = ref.prevOffsets
				s.out = s.out[:0]
				err := s.decodeSync(hist)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(s.out)))
				decoded += ref.n
			}
			b.ReportMetric(float64(decoded)/time.Since(t).Seconds(), "seq/s")
		})
	}
}

func testCreateZipReader(path string, tb testing.TB) *zip.Reader {
	failOnError := func(err error) {
		if err != nil {
			tb.Fatal(err)
		}
	}

	data, err := os.ReadFile(path)
	failOnError(err)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	failOnError(err)

	return zr
}

// TestDecoderShortSequenceCopies round-trips inputs built so that their
// sequences cluster at the boundaries the assembly copy paths treat
// differently: literal runs of 0, 16 and 17 bytes and matches of 16 and 17
// bytes. The extended copies write the first 16-byte block of every literal
// run and match unconditionally and loop only past 16 bytes, so a bug there
// shows up as corruption at exactly these lengths. The inputs are random
// runs of the stated length between repeated words of the stated length; the
// encoder decides the actual sequences, so the lengths cluster at rather than
// equal the boundary. That the clustering is close enough is checked by
// mutation: with the match tail threshold in the generator off by one, the
// 17-byte-word cases fail with a CRC error.
//
// Both the DecodeAll path (decodeSync) and the streaming path (decode +
// executeSimple) are exercised. Both allocate their buffers with
// compressedBlockOverAlloc slack and so run the extended copies; the
// selection itself is asserted by Test_seqdec_decodeSync on recorded blocks.
func TestDecoderShortSequenceCopies(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	randomBytes := func(n int) []byte {
		b := make([]byte, n)
		rng.Read(b)
		return b
	}
	// Enough for several blocks so matches also reach into block history.
	const inputSize = 300 << 10

	tests := []struct {
		name     string
		wordLens []int // lengths of the repeated words the encoder should match
		litLens  []int // lengths of the random runs between words
	}{
		{name: "words16-lits0", wordLens: []int{16}, litLens: []int{0}},
		{name: "words17-lits0", wordLens: []int{17}, litLens: []int{0}},
		{name: "words16-lits16", wordLens: []int{16}, litLens: []int{16}},
		{name: "words17-lits17", wordLens: []int{17}, litLens: []int{17}},
		{name: "words15-lits1", wordLens: []int{15}, litLens: []int{1}},
		{name: "mixed", wordLens: []int{4, 5, 8, 15, 16, 17, 31, 32, 33}, litLens: []int{0, 0, 0, 1, 2, 15, 16, 17, 32, 33}},
	}
	levels := []EncoderLevel{SpeedFastest, SpeedDefault, SpeedBetterCompression, SpeedBestCompression}

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	for _, tc := range tests {
		var words [][]byte
		for _, n := range tc.wordLens {
			for range 8 {
				words = append(words, randomBytes(n))
			}
		}
		input := make([]byte, 0, inputSize+64)
		for len(input) < inputSize {
			if n := tc.litLens[rng.Intn(len(tc.litLens))]; n > 0 {
				input = append(input, randomBytes(n)...)
			}
			input = append(input, words[rng.Intn(len(words))]...)
		}

		for _, level := range levels {
			t.Run(tc.name+"/"+level.String(), func(t *testing.T) {
				enc, err := NewWriter(nil, WithEncoderLevel(level))
				if err != nil {
					t.Fatal(err)
				}
				compressed := enc.EncodeAll(input, nil)
				enc.Close()

				got, err := dec.DecodeAll(compressed, nil)
				if err != nil {
					t.Fatalf("DecodeAll: %v", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatalf("DecodeAll output mismatch (len %d vs %d)", len(got), len(input))
				}

				if err := dec.Reset(bytes.NewReader(compressed)); err != nil {
					t.Fatal(err)
				}
				got, err = io.ReadAll(dec)
				if err != nil {
					t.Fatalf("streaming decode: %v", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatalf("streaming output mismatch (len %d vs %d)", len(got), len(input))
				}
			})
		}
	}
}

// twoPassTestInput builds ~6 MiB whose matches reach across the whole
// window: random vocabulary text, then copies from up to 4 MiB back.
func twoPassTestInput() []byte {
	rng := rand.New(rand.NewSource(7))
	vocab := make([][]byte, 4096)
	for i := range vocab {
		w := make([]byte, 3+rng.Intn(8))
		for j := range w {
			w[j] = byte('a' + rng.Intn(26))
		}
		vocab[i] = w
	}
	buf := make([]byte, 0, 6<<20)
	for len(buf) < 4<<20 {
		buf = append(buf, vocab[rng.Intn(len(vocab))]...)
		buf = append(buf, " ,.\n"[rng.Intn(4)])
	}
	for len(buf) < 6<<20 {
		for i := rng.Intn(4); i > 0; i-- {
			buf = append(buf, byte(rng.Intn(256)))
		}
		n := 4 + rng.Intn(60)
		off := 1 + rng.Intn(4<<20)
		src := len(buf) - off
		buf = append(buf, buf[src:src+n]...)
	}
	return buf
}

// forceTwoPass makes the synchronous decoder take the two-pass path on every
// block (on) or the one-pass path (off) whatever the data, and returns a
// function restoring the thresholds.
func forceTwoPass(on bool) func() {
	a, b := decodeTwoPassMinWindow, twoPassMinFarShare
	if on {
		decodeTwoPassMinWindow, twoPassMinFarShare = 0, 0
	} else {
		decodeTwoPassMinWindow, twoPassMinFarShare = math.MaxInt, 257
	}
	return func() { decodeTwoPassMinWindow, twoPassMinFarShare = a, b }
}

// TestSequenceDecsUseTwoPass checks the two-pass decision against each kind
// of offset table.
func TestSequenceDecsUseTwoPass(t *testing.T) {
	defer forceTwoPass(false)()
	decodeTwoPassMinWindow, twoPassMinFarShare = 1<<20, 46

	// fseTable: log-8 table with far slots on codes 20-22 (two of them -1),
	// the rest on code 3.
	fseTable := func(far int) *fseDecoder {
		f := &fseDecoder{actualTableLog: 8, symbolLen: twoPassFarCode + 3}
		f.norm[3] = int16(256 - far)
		f.norm[twoPassFarCode] = int16(far - 2)
		f.norm[twoPassFarCode+1] = -1
		f.norm[twoPassFarCode+2] = -1
		return f
	}
	rle := func(code uint8) *fseDecoder {
		return &fseDecoder{actualTableLog: 0, maxBits: code}
	}
	predefined := &fseDecoder{preDefined: true}

	tests := []struct {
		name   string
		fse    *fseDecoder
		window int
		want   bool
	}{
		{"fse at threshold", fseTable(46), 1 << 20, true},
		{"fse at threshold small window", fseTable(46), 1<<20 - 1, false},
		{"fse below threshold", fseTable(45), 1 << 30, false},
		{"fse far only", fseTable(256), 1 << 20, true},
		{"fse none far", fseTable(2), 1 << 30, false},
		{"fse no far codes in table", &fseDecoder{actualTableLog: 8, symbolLen: twoPassFarCode}, 1 << 20, false},
		{"rle large window", rle(uint8(twoPassFarCode - 1)), 1 << 20, true},
		{"rle small window", rle(uint8(twoPassFarCode)), 1<<20 - 1, false},
		{"predefined small window", predefined, 1<<20 - 1, false},
		{"predefined large window", predefined, 1 << 20, true},
		{"no table small window", nil, 1<<20 - 1, false},
		{"no table large window", nil, 1 << 20, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := sequenceDecs{windowSize: tt.window}
			s.offsets.fse = tt.fse
			if got := s.useTwoPass(); got != tt.want {
				t.Errorf("useTwoPass = %v, want %v (share %d)", got, tt.want, tt.fse.codeShare(twoPassFarCode))
			}
		})
	}
}

// TestDecodeTwoPassMatchesOnePass forces both the two-pass and the one-pass
// path on any architecture and checks every DecodeAll and Reader shape, plus
// rejection of corrupt input, on each.
func TestDecodeTwoPassMatchesOnePass(t *testing.T) {
	input := twoPassTestInput()
	frames := map[string][]byte{}
	for _, lvl := range []EncoderLevel{SpeedDefault, SpeedBestCompression} {
		enc, err := NewWriter(nil, WithEncoderLevel(lvl), WithWindowSize(8<<20))
		if err != nil {
			t.Fatal(err)
		}
		frames[lvl.String()] = enc.EncodeAll(input, nil)
		enc.Close()
	}

	paths := []struct {
		name string
		on   bool
	}{
		{"two-pass+prefetch", true},
		{"one-pass", false},
	}
	for _, p := range paths {
		for name, comp := range frames {
			t.Run(p.name+"/"+name, func(t *testing.T) {
				defer forceTwoPass(p.on)()

				dec, err := NewReader(nil, WithDecoderConcurrency(1))
				if err != nil {
					t.Fatal(err)
				}
				defer dec.Close()

				got, err := dec.DecodeAll(comp, nil)
				if err != nil {
					t.Fatal("DecodeAll:", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatal("DecodeAll output differs from input")
				}

				// A non-empty dst puts the history in a separate buffer.
				prefix := []byte("prefix bytes that are not part of the frame")
				got, err = dec.DecodeAll(comp, append([]byte(nil), prefix...))
				if err != nil {
					t.Fatal("DecodeAll with prefix:", err)
				}
				if !bytes.Equal(got, append(append([]byte(nil), prefix...), input...)) {
					t.Fatal("DecodeAll with prefix: output differs from input")
				}

				// Exactly sized dst.
				got, err = dec.DecodeAll(comp, make([]byte, 0, len(input)))
				if err != nil {
					t.Fatal("DecodeAll exact:", err)
				}
				if !bytes.Equal(got, input) {
					t.Fatal("DecodeAll exact: output differs from input")
				}

				for _, n := range []int{1, 2} {
					rdr, err := NewReader(bytes.NewReader(comp), WithDecoderConcurrency(n))
					if err != nil {
						t.Fatal(err)
					}
					got, err = io.ReadAll(rdr)
					rdr.Close()
					if err != nil {
						t.Fatalf("Reader concurrency %d: %v", n, err)
					}
					if !bytes.Equal(got, input) {
						t.Fatalf("Reader concurrency %d: output differs from input", n)
					}
				}

				// Corrupt input must error, not panic.
				bad := append([]byte(nil), comp...)
				for i := len(bad) / 3; i < len(bad); i += len(bad) / 7 {
					bad[i] ^= 0x5a
				}
				if _, err := dec.DecodeAll(bad, nil); err == nil {
					t.Fatal("corrupted frame decoded without error")
				}
				if _, err := dec.DecodeAll(comp[:len(comp)*2/3], nil); err == nil {
					t.Fatal("truncated frame decoded without error")
				}
			})
		}
	}
}
