package huff0

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/klauspost/compress/internal/cpuinfo"
	"github.com/klauspost/compress/internal/fuzz"
)

func FuzzCompress(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/fse_compress.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/regression.zip", fuzz.TypeRaw, testing.Short())
	// Reuse the Scratches across inputs, as zstd does. Each Scratch's buffer
	// pool keeps it alive until the second GC after use, so one per input
	// grows the worker's heap until it is OOM-killed.
	var s, decS Scratch
	f.Fuzz(func(t *testing.T, buf0 []byte) {
		//use of Compress1X
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
