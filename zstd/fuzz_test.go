package zstd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	rdebug "runtime/debug"
	"testing"

	"github.com/klauspost/compress/internal/cpuinfo"
	"github.com/klauspost/compress/internal/fuzz"
)

func FuzzDecodeAll(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/decode-regression.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/fuzz/decode-corpus-raw.zip", fuzz.TypeRaw, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/decode-corpus-encoded.zip", fuzz.TypeGoFuzz, testing.Short())

	// Also compare the BMI2 and non-BMI2 assembly variants where available.
	const compareBMI2 = true

	f.Fuzz(func(t *testing.T, b []byte) {
		// Just test if we crash...
		defer func() {
			if r := recover(); r != nil {
				rdebug.PrintStack()
				t.Fatal(r)
			}
		}()

		decLow, err := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(2), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true))
		if err != nil {
			t.Fatal(err)
		}
		defer decLow.Close()
		decHi, err := NewReader(nil, WithDecoderLowmem(false), WithDecoderConcurrency(2), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true))
		if err != nil {
			t.Fatal(err)
		}
		defer decHi.Close()
		b1, err1 := decLow.DecodeAll(b, make([]byte, 0, len(b)))
		b2, err2 := decHi.DecodeAll(b, make([]byte, 0, len(b)))
		if err1 != err2 {
			if (err1 == nil) != (err2 == nil) {
				t.Errorf("err low: %v, hi: %v", err1, err2)
			}
		}
		if err1 != nil {
			b1, b2 = b1[:0], b2[:0]
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("Output mismatch, low: %v, hi: %v", err1, err2)
		}

		decCap, err := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(1), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true), WithDecodeAllCapLimit(true))
		if err != nil {
			t.Fatal(err)
		}
		defer decCap.Close()
		limit, prefixLen := min(len(b), 1024), len(b)&7
		prefix := bytes.Repeat([]byte{0xa5}, prefixLen)
		dst := make([]byte, prefixLen, prefixLen+limit)
		copy(dst, prefix)
		got, capErr := decCap.DecodeAll(b, dst)
		if len(got) > cap(dst) || cap(got) > cap(dst) {
			t.Fatalf("capacity limit exceeded: len=%d cap=%d limit=%d", len(got), cap(got), cap(dst))
		}
		if capErr == nil && err1 != nil {
			t.Fatalf("capacity-limited decoder accepted input rejected without the limit: %v", err1)
		}
		if err1 == nil {
			if len(b1) > limit {
				if !errors.Is(capErr, ErrDecoderSizeExceeded) {
					t.Fatalf("expected capacity error, got %v", capErr)
				}
			} else if capErr != nil || len(got) < prefixLen || !bytes.Equal(got[:prefixLen], prefix) || !bytes.Equal(got[prefixLen:], b1) {
				t.Fatalf("capacity-limited output differs: %v", capErr)
			}
		}

		decodeSync := func(goOnly bool) ([]byte, error) {
			old := decodeSyncGoOnly
			decodeSyncGoOnly = goOnly
			defer func() { decodeSyncGoOnly = old }()
			dec, err := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(1), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true))
			if err != nil {
				t.Fatal(err)
			}
			defer dec.Close()
			// Unknown-size frames need a full block of spare capacity to select
			// the assembly sequence decoder rather than its Go fallback.
			return dec.DecodeAll(b, make([]byte, 0, max(len(b), maxCompressedBlockSize)))
		}
		asmOut, asmErr := decodeSync(false)
		goOut, goErr := decodeSync(true)
		if (asmErr == nil) != (goErr == nil) {
			t.Fatalf("sequence decoder acceptance mismatch: asm=%v Go=%v", asmErr, goErr)
		}
		if asmErr == nil && !bytes.Equal(asmOut, goOut) {
			t.Fatal("assembly vs Go sequence decoder output mismatch")
		}

		if compareBMI2 && cpuinfo.HasBMI2() {
			// Restore BMI2 on every exit path.
			defer cpuinfo.DisableBMI2()()
			decRef, errRef := NewReader(nil, WithDecoderLowmem(true), WithDecoderConcurrency(1), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true))
			if errRef != nil {
				t.Fatal(errRef)
			}
			defer decRef.Close()
			bRef, errRef := decRef.DecodeAll(b, make([]byte, 0, len(b)))
			if (err1 == nil) != (errRef == nil) {
				t.Errorf("err BMI2: %v, non-BMI2: %v", err1, errRef)
			}
			if err1 == nil && !bytes.Equal(b1, bRef) {
				t.Fatalf("BMI2 vs non-BMI2 output mismatch")
			}
		}
	})
}

func FuzzDecAllNoBMI2(f *testing.F) {
	if !cpuinfo.HasBMI2() {
		f.Skip("No BMI, so already tested")
		return
	}
	defer cpuinfo.DisableBMI2()()
	FuzzDecodeAll(f)
}

func FuzzDecoder(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/fuzz/decode-corpus-raw.zip", fuzz.TypeRaw, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/decode-corpus-encoded.zip", fuzz.TypeGoFuzz, testing.Short())
	//fuzz.AddFromZip(f, "testdata/fuzz/decode-oss.zip", fuzz.TypeOSSFuzz, false)

	brLow := newBytesReader(nil)
	brHi := newBytesReader(nil)
	brFrames := newBytesReader(nil)
	brCRC := newBytesReader(nil)
	f.Fuzz(func(t *testing.T, b []byte) {
		// Just test if we crash...
		defer func() {
			if r := recover(); r != nil {
				rdebug.PrintStack()
				t.Fatal(r)
			}
		}()
		brLow.Reset(b)
		brHi.Reset(b)
		brFrames.Reset(b)
		brCRC.Reset(b)
		decLow, err := NewReader(brLow, WithDecoderLowmem(true), WithDecoderConcurrency(2), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true), WithDecodeBuffersBelow(8<<10))
		if err != nil {
			t.Fatal(err)
		}
		defer decLow.Close()

		// Test with high memory, but sync decoding
		decHi, err := NewReader(brHi, WithDecoderLowmem(false), WithDecoderConcurrency(1), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), IgnoreChecksum(true), WithDecodeBuffersBelow(8<<10))
		if err != nil {
			t.Fatal(err)
		}
		defer decHi.Close()

		// Concurrent frames, with a small limit to also hand off to the stream
		// decoder, checking checksums against a sync decoder that does too.
		// Hide Bytes so small inputs are not decoded with DecodeAll.
		decFrames, err := NewReader(struct{ io.Reader }{brFrames}, WithDecoderConcurrency(2), WithDecodeFramesConcurrently(256<<10), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), WithDecodeBuffersBelow(8<<10))
		if err != nil {
			t.Fatal(err)
		}
		defer decFrames.Close()
		decCRC, err := NewReader(brCRC, WithDecoderConcurrency(1), WithDecoderMaxMemory(20<<20), WithDecoderMaxWindow(1<<20), WithDecodeBuffersBelow(8<<10))
		if err != nil {
			t.Fatal(err)
		}
		defer decCRC.Close()

		if debugDecoder {
			fmt.Println("LOW CONCURRENT")
		}
		b1, err1 := io.ReadAll(decLow)

		if debugDecoder {
			fmt.Println("HI NOT CONCURRENT")
		}
		b2, err2 := io.ReadAll(decHi)
		b3, err3 := io.ReadAll(decFrames)
		b4, err4 := io.ReadAll(decCRC)
		if (err3 == nil) != (err4 == nil) {
			t.Errorf("err frames concurrent: %v, sync with checksums: %v", err3, err4)
		}
		// On error, the stream decoder may return more of a frame than the sync one.
		if err4 == nil && !bytes.Equal(b4, b3) || !bytes.HasPrefix(b4, b3) && !bytes.HasPrefix(b3, b4) {
			t.Fatalf("Output mismatch, frames concurrent: %v, sync with checksums: %v", err3, err4)
		}

		if err1 != err2 {
			if (err1 == nil) != (err2 == nil) {
				t.Errorf("err low concurrent: %v, hi: %v", err1, err2)
			}
		}
		if err1 != nil {
			b1, b2 = b1[:0], b2[:0]
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("Output mismatch, low concurrent: %v, hi: %v", err1, err2)
		}
	})
}

func FuzzNoBMI2Dec(f *testing.F) {
	if !cpuinfo.HasBMI2() {
		f.Skip("No BMI, so already tested")
		return
	}
	defer cpuinfo.DisableBMI2()()
	FuzzDecoder(f)
}

func FuzzEncoding(f *testing.F) {
	// Fuzzing tweaks:
	const (
		// Test a subset of encoders.
		startFuzz = SpeedFastest
		endFuzz   = SpeedUltraCompression

		// Also tests with dictionaries...
		testDicts = true
	)
	// Inputs ending in flushMarker have it removed, and are written to the
	// dictionary encoders in flushed pieces, so frames have several blocks.
	flushMarker := []byte{1, 2, 3, 4}
	addWithMarker := func(filename string, typ fuzz.InputType) {
		i := 0
		fuzz.ReturnFromZip(f, filename, typ, func(b []byte) {
			defer func() { i++ }()
			if testing.Short() && i%10 != 0 {
				return
			}
			f.Add(b)
			if testDicts {
				f.Add(append(b, flushMarker...))
			}
		})
	}
	addWithMarker("testdata/fuzz/encode-corpus-raw.zip", fuzz.TypeRaw)
	fuzz.AddFromZip(f, "testdata/comp-crashers.zip", fuzz.TypeRaw, false)
	addWithMarker("testdata/fuzz/encode-corpus-encoded.zip", fuzz.TypeGoFuzz)

	var dec *Decoder
	var encs [endFuzz + 1]*Encoder
	var encsD [endFuzz + 1]*Encoder

	var dicts [][]byte
	if testDicts {
		zr := testCreateZipReader("testdata/dict-tests-small.zip", f)
		dicts = readDicts(f, zr)
	}

	if testing.Short() && *fuzzEndF > int(SpeedBetterCompression) {
		*fuzzEndF = int(SpeedBetterCompression)
	}

	initEnc := func() func() {
		var err error
		dec, err = NewReader(nil, WithDecoderConcurrency(2), WithDecoderDicts(dicts...), WithDecoderMaxWindow(64<<10), WithDecoderMaxMemory(uint64(*fuzzMaxF)))
		if err != nil {
			panic(err)
		}
		for level := startFuzz; level <= endFuzz; level++ {
			encs[level], err = NewWriter(nil, WithEncoderCRC(true), WithEncoderLevel(level), WithEncoderConcurrency(2), WithWindowSize(64<<10), WithZeroFrames(true), WithLowerEncoderMem(true))
			if testDicts {
				encsD[level], err = NewWriter(nil, WithEncoderCRC(true), WithEncoderLevel(level), WithEncoderConcurrency(2), WithWindowSize(64<<10), WithZeroFrames(true), WithEncoderDict(dicts[int(level)%len(dicts)]), WithLowerEncoderMem(true), WithLowerEncoderMem(true))
			}
		}
		return func() {
			dec.Close()
			for _, enc := range encs {
				if enc != nil {
					enc.Close()
				}
			}
			if testDicts {
				for _, enc := range encsD {
					if enc != nil {
						enc.Close()
					}
				}
			}
		}
	}

	f.Cleanup(initEnc())

	var dst bytes.Buffer

	f.Fuzz(func(t *testing.T, data []byte) {
		// Just test if we crash...
		defer func() {
			if r := recover(); r != nil {
				stack := rdebug.Stack()
				t.Fatalf("%v:\n%v", r, string(stack))
			}
		}()
		if len(data) > *fuzzMaxF {
			return
		}
		flushed := testDicts && bytes.HasSuffix(data, flushMarker)
		if flushed {
			data = data[:len(data)-len(flushMarker)]
		}
		var bufSize = len(data)
		if bufSize > 2 {
			// Make deterministic size
			bufSize = int(data[0]) | int(data[1])<<8
			if bufSize >= len(data) {
				bufSize = len(data) / 2
			}
		}

		// Best and ultra split blocks, and cut them into parts of every sequences.
		every := 0
		if len(data) > 2 && data[2]&64 == 0 {
			every = int(data[2] & 63)
		}

		for level := *fuzzStartF; level <= *fuzzEndF; level++ {
			if EncoderLevel(level) == SpeedUltraCompression && len(data) > 64<<10 {
				continue
			}
			split := EncoderLevel(level) >= SpeedBestCompression
			enc := encs[level]
			dst.Reset()
			enc.Reset(&dst)
			if split {
				setSplitSpy(enc, every)
			}
			n, err := enc.Write(data)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(data) {
				t.Fatal(fmt.Sprintln("Level", level, "Short write, got:", n, "want:", len(data)))
			}

			encoded := enc.EncodeAll(data, make([]byte, 0, bufSize))
			if len(encoded) > enc.MaxEncodedSize(len(data)) {
				t.Errorf("max encoded size for %v: got: %d, want max: %d", len(data), len(encoded), enc.MaxEncodedSize(len(data)))
			}

			got, err := dec.DecodeAll(encoded, make([]byte, 0, bufSize))
			if err != nil {
				t.Fatal(fmt.Sprintln("Level", level, "DecodeAll error:", err, "\norg:", len(data), "\nencoded", len(encoded)))
			}
			if !bytes.Equal(got, data) {
				t.Fatal(fmt.Sprintln("Level", level, "DecodeAll output mismatch\n", len(got), "org: \n", len(data), "(want)", "\nencoded:", len(encoded)))
			}

			err = enc.Close()
			if err != nil {
				t.Fatal(fmt.Sprintln("Level", level, "Close (buffer) error:", err))
			}
			encoded2 := dst.Bytes()
			if len(encoded2) > enc.MaxEncodedSize(len(data)) {
				t.Errorf("max encoded size for %v: got: %d, want max: %d", len(data), len(encoded2), enc.MaxEncodedSize(len(data)))
			}
			if !bytes.Equal(encoded, encoded2) {
				got, err = dec.DecodeAll(encoded2, got[:0])
				if err != nil {
					t.Fatal(fmt.Sprintln("Level", level, "DecodeAll (buffer) error:", err, "\norg:", len(data), "\nencoded", len(encoded2)))
				}
				if !bytes.Equal(got, data) {
					t.Fatal(fmt.Sprintln("Level", level, "DecodeAll (buffer) output mismatch\n", len(got), "org: \n", len(data), "(want)", "\nencoded:", len(encoded2)))
				}
			}
			if !testDicts {
				continue
			}
			enc = encsD[level]
			dst.Reset()
			enc.Reset(&dst)
			if split {
				setSplitSpy(enc, every)
			}
			if flushed {
				// At most 256 pieces, so large inputs don't flush every byte.
				for todo, piece := data, max(1+bufSize%2048, (len(data)+255)/256); len(todo) > 0; todo = todo[n:] {
					n, err = enc.Write(todo[:min(piece, len(todo))])
					if err != nil {
						t.Fatal(err)
					}
					if n != min(piece, len(todo)) {
						t.Fatal(fmt.Sprintln("Dict Level", level, "Short write, got:", n, "want:", min(piece, len(todo))))
					}
					if err := enc.Flush(); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				n, err = enc.Write(data)
				if err != nil {
					t.Fatal(err)
				}
				if n != len(data) {
					t.Fatal(fmt.Sprintln("Dict Level", level, "Short write, got:", n, "want:", len(data)))
				}
			}

			encoded = enc.EncodeAll(data, encoded[:0])
			if len(encoded) > enc.MaxEncodedSize(len(data)) {
				t.Errorf("max encoded size for %v: got: %d, want max: %d", len(data), len(encoded), enc.MaxEncodedSize(len(data)))
			}
			got, err = dec.DecodeAll(encoded, got[:0])
			if err != nil {
				t.Fatal(fmt.Sprintln("Dict Level", level, "DecodeAll error:", err, "\norg:", len(data), "\nencoded", len(encoded)))
			}
			if !bytes.Equal(got, data) {
				t.Fatal(fmt.Sprintln("Dict Level", level, "DecodeAll output mismatch\n", len(got), "org: \n", len(data), "(want)", "\nencoded:", len(encoded)))
			}

			err = enc.Close()
			if err != nil {
				t.Fatal(fmt.Sprintln("Dict Level", level, "Close (buffer) error:", err))
			}
			encoded2 = dst.Bytes()
			// MaxEncodedSize does not count the blocks that flushes add.
			if !flushed && len(encoded2) > enc.MaxEncodedSize(len(data)) {
				t.Errorf("max encoded size for %v: got: %d, want max: %d", len(data), len(encoded2), enc.MaxEncodedSize(len(data)))
			}
			if !bytes.Equal(encoded, encoded2) {
				got, err = dec.DecodeAll(encoded2, got[:0])
				if err != nil {
					t.Fatal(fmt.Sprintln("Dict Level", level, "DecodeAll (buffer) error:", err, "\norg:", len(data), "\nencoded", len(encoded2)))
				}
				if !bytes.Equal(got, data) {
					t.Fatal(fmt.Sprintln("Dict Level", level, "DecodeAll (buffer) output mismatch\n", len(got), "org: \n", len(data), "(want)", "\nencoded:", len(encoded2)))
				}
			}
		}
	})
}
