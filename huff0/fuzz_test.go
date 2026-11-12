package huff0

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/klauspost/compress/internal/cpuinfo"
	"github.com/klauspost/compress/internal/fuzz"
)

func FuzzCompress(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/fse_compress.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/regression.zip", fuzz.TypeRaw, testing.Short())
	// A table from the text has long codes for 1-4, so ReusePolicyPrefer
	// falls back to a new table.
	prev := strings.Repeat("the quick brown fox jumps over the lazy dog ", 100) + "\x01\x02\x03\x04"
	seed := append([]byte{byte(len(prev)), byte(len(prev) >> 8)}, prev...)
	rng := rand.New(rand.NewSource(1))
	for range 2000 {
		seed = append(seed, 1+byte(rng.Intn(4)))
	}
	f.Add(seed)
	// Reuse the Scratches across inputs, as zstd does. Each Scratch's buffer
	// pool keeps it alive until the second GC after use, so one per input
	// grows the worker's heap until it is OOM-killed.
	var s, decS Scratch
	f.Fuzz(func(t *testing.T, buf0 []byte) {
		fuzzReuse(t, buf0, &s, &decS)

		//use of Compress1X
		s.WantLogLess = 0
		s.Reuse = ReusePolicyNone
		s.prevTable = s.prevTable[:0]
		if len(buf0) > BlockSizeMax {
			buf0 = buf0[:BlockSizeMax]
		}
		EstimateSizes(buf0, &s)
		b, re, err := Compress1X(buf0, &s)
		s.validateTable(s.cTable)
		s.canUseTable(s.cTable)
		if err != nil || b == nil {
			return
		}

		min := s.minSize(len(buf0))

		if len(s.OutData) < min {
			t.Errorf("FuzzCompress: output data length (%d) below shannon limit (%d)", len(s.OutData), min)
		}
		if lb := s.minNewSize(len(buf0)); len(b) < lb {
			t.Errorf("FuzzCompress: output length (%d) below lower bound (%d)", len(b), lb)
		}
		if len(s.OutTable) == 0 {
			t.Error("FuzzCompress: got no table definition")
		}
		if re {
			t.Error("FuzzCompress: claimed to have re-used.")
		}
		if len(s.OutData) == 0 {
			t.Error("FuzzCompress: got no data output")
		}

		dec, remain, err := ReadTable(b, &decS)

		//use of Decompress1X
		out, err := dec.Decompress1X(remain)
		if err != nil || len(out) == 0 {
			return
		}
		if !bytes.Equal(out, buf0) {
			t.Fatal(fmt.Sprintln("FuzzCompressX1 output mismatch\n", len(out), "org: \n", len(buf0)))
		}

		//use of Compress4X
		s.Reuse = ReusePolicyAllow
		b, reUsed, err := Compress4X(buf0, &s)
		if err != nil || b == nil {
			return
		}
		remain = b
		if !reUsed {
			dec, remain, err = ReadTable(b, dec)
			if err != nil {
				return
			}
		}
		//use of Decompress4X
		out, err = dec.Decompress4X(remain, len(buf0))
		if err != nil || out == nil {
			return
		}
		if !bytes.Equal(out, buf0) {
			t.Fatal(fmt.Sprintln("FuzzCompressX4 output mismatch: ", len(out), ", org: ", len(buf0)))
		}
		// Decompress4X has a BMI2 twin on amd64; cover the generic one too.
		func() {
			defer cpuinfo.DisableBMI2()()
			out, err := dec.Decompress4X(remain, len(buf0))
			if err != nil {
				t.Fatal("FuzzCompressX4 no-BMI2 decode failed:", err)
			}
			if !bytes.Equal(out, buf0) {
				t.Fatal(fmt.Sprintln("FuzzCompressX4 no-BMI2 output mismatch: ", len(out), ", org: ", len(buf0)))
			}
		}()
	})
}

// fuzzReuse compresses part of data with a table from the rest, with every
// reuse policy. The first 2 bytes choose the split and WantLogLess.
func fuzzReuse(t *testing.T, data []byte, s, prevDec *Scratch) {
	if len(data) < 3 {
		return
	}
	split := int(data[0]) | int(data[1])<<8
	wll := data[0] & 7
	data = data[2:]
	if split > len(data) {
		split = len(data) / 2
	}
	prev, in := data[:split], data[split:]
	prev = prev[:min(len(prev), BlockSizeMax)]
	in = in[:min(len(in), BlockSizeMax)]
	for _, four := range []bool{false, true} {
		comp := Compress1X
		if four {
			comp = Compress4X
		}
		newOK := false
		for _, p := range []ReusePolicy{ReusePolicyNone, ReusePolicyAllow, ReusePolicyPrefer, ReusePolicyMust} {
			s.Reuse = ReusePolicyNone
			s.WantLogLess = wll
			pout, _, perr := comp(prev, s)
			var pd *Scratch
			if perr == nil {
				var err error
				if pd, _, err = ReadTable(pout, prevDec); err != nil {
					t.Fatal("prev table:", err)
				}
			}
			s.Reuse = p
			out, reused, err := comp(in, s)
			if p == ReusePolicyNone {
				newOK = err == nil
			}
			if err != nil {
				// Prefer falls back to a new table.
				if p == ReusePolicyPrefer && newOK {
					t.Fatalf("four=%v: Prefer failed with %v, but a new table works", four, err)
				}
				continue
			}
			wantSize := len(in)
			if wll > 0 {
				wantSize -= wantSize >> wll
			}
			if len(out) >= wantSize {
				t.Fatalf("four=%v policy=%d: out %d >= want %d", four, p, len(out), wantSize)
			}
			dec, rem := pd, out
			if reused {
				if pd == nil {
					t.Fatalf("four=%v policy=%d: reused without a previous table", four, p)
				}
				if len(s.OutTable) != 0 || !bytes.Equal(s.OutData, out) {
					t.Fatalf("four=%v policy=%d: reused, but OutTable %d, OutData %d, out %d", four, p, len(s.OutTable), len(s.OutData), len(out))
				}
			} else {
				if p == ReusePolicyMust {
					t.Fatal("Must did not reuse")
				}
				if len(s.OutTable)+len(s.OutData) != len(out) {
					t.Fatalf("four=%v policy=%d: OutTable %d + OutData %d != out %d", four, p, len(s.OutTable), len(s.OutData), len(out))
				}
				if dec, rem, err = ReadTable(out, prevDec); err != nil {
					t.Fatalf("four=%v policy=%d: ReadTable: %v", four, p, err)
				}
			}
			var got []byte
			if four {
				got, err = dec.Decompress4X(rem, len(in))
			} else {
				got, err = dec.Decompress1X(rem)
			}
			if err != nil {
				t.Fatalf("four=%v policy=%d reused=%v: decode: %v", four, p, reused, err)
			}
			if !bytes.Equal(got, in) {
				t.Fatalf("four=%v policy=%d reused=%v: mismatch", four, p, reused)
			}
		}
	}
}

func FuzzDecompress1x(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/huff0_decompress1x.zip", fuzz.TypeRaw, false)

	// Seed with real huff0 output, so the decoder sees well-formed tables
	// and payloads, not just whatever ReadTable happens to accept.
	var seed Scratch
	addCompressed := func(b []byte) {
		seed.Reuse = ReusePolicyNone
		if b2, _, err := Compress1X(b, &seed); err == nil {
			f.Add(b2)
		}
		seed.Reuse = ReusePolicyNone
		if b2, _, err := Compress4X(b, &seed); err == nil {
			f.Add(b2)
		}
	}
	fuzz.ReturnFromZip(f, "testdata/regression.zip", fuzz.TypeRaw, addCompressed)
	fuzz.ReturnFromZip(f, "testdata/fse_compress.zip", fuzz.TypeRaw, addCompressed)

	var s Scratch // Reused across inputs; see FuzzCompress.
	f.Fuzz(func(t *testing.T, buf0 []byte) {
		_, remain, err := ReadTable(buf0, &s)
		if err != nil {
			return
		}
		s.Decompress1X(remain)
		s.Decompress4X(remain, len(buf0))
	})
}
