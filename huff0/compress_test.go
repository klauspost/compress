package huff0

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/flate"
)

type inputFn func() ([]byte, error)

var testfiles = []struct {
	name  string
	fn    inputFn
	err1X error
	err4X error
}{
	// Digits is the digits of the irrational number e. Its decimal representation
	// does not repeat, but there are only 10 possible digits, so it should be
	// reasonably compressible.
	{name: "digits", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/e.txt") }},
	// gettysburg.txt is a small plain text.
	{name: "gettysburg", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/gettysburg.txt") }},
	// Twain is Project Gutenberg's edition of Mark Twain's classic English novel.
	{name: "twain", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt") }},
	// Random bytes
	{name: "random", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/sharnd.out") }, err1X: ErrIncompressible, err4X: ErrIncompressible},
	// Low entropy
	{name: "low-ent.10k", fn: func() ([]byte, error) { return []byte(strings.Repeat("1221", 10000)), nil }},
	// Super Low entropy
	{name: "superlow-ent-10k", fn: func() ([]byte, error) { return []byte(strings.Repeat("1", 10000) + strings.Repeat("2", 500)), nil }},
	// Zero bytes
	{name: "zeroes", fn: func() ([]byte, error) { return make([]byte, 10000), nil }, err1X: ErrUseRLE, err4X: ErrUseRLE},
	{name: "crash1", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/crash1.bin") }, err1X: ErrIncompressible, err4X: ErrIncompressible},
	{name: "crash2", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/crash2.bin") }, err4X: ErrIncompressible},
	{name: "crash3", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/crash3.bin") }, err1X: ErrIncompressible, err4X: ErrIncompressible},
	{name: "endzerobits", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/endzerobits.bin") }, err1X: nil, err4X: ErrIncompressible},
	{name: "endnonzero", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/endnonzero.bin") }, err4X: ErrIncompressible},
	{name: "case1", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/case1.bin") }, err1X: nil},
	{name: "case2", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/case2.bin") }, err1X: nil},
	{name: "case3", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/case3.bin") }, err1X: nil},
	{name: "pngdata.001", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/pngdata.bin") }, err1X: nil},
	{name: "normcount2", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/normcount2.bin") }, err1X: nil},
}

type fuzzInput struct {
	name string
	fn   inputFn
}

var testfilesExtended []fuzzInput

func init() {
	filepath.Walk("./fuzz/compress/corpus", func(path string, info os.FileInfo, err error) error {
		if info.Size() == 0 || info.IsDir() {
			return nil
		}
		testfilesExtended = append(testfilesExtended, fuzzInput{
			name: filepath.Base(path),
			fn: func() ([]byte, error) {
				return ioutil.ReadFile(path)
			},
		})
		return nil
	})
}

func TestCompress1X(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			var s Scratch
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress1X(buf0, &s)
			if err != test.err1X {
				t.Errorf("want error %v (%T), got %v (%T)", test.err1X, test.err1X, err, err)
			}
			if err != nil {
				t.Log(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			min := s.minSize(len(buf0))
			if len(s.OutData) < min {
				t.Errorf("output data length (%d) below shannon limit (%d)", len(s.OutData), min)
			}
			if len(s.OutTable) == 0 {
				t.Error("got no table definition")
			}
			if re {
				t.Error("claimed to have re-used.")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}
			t.Logf("%s: %d -> %d bytes (%.2f:1) re:%t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))
			s.Out = nil
			bRe, _, err := Compress1X(b, &s)
			if err == nil {
				t.Log("Could re-compress to", len(bRe))
			}
		})
	}
}

func TestCompress4X(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			var s Scratch
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress4X(buf0, &s)
			if err != test.err4X {
				t.Errorf("want error %v (%T), got %v (%T)", test.err1X, test.err4X, err, err)
			}
			if err != nil {
				t.Log(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			if len(s.OutTable) == 0 {
				t.Error("got no table definition")
			}
			if re {
				t.Error("claimed to have re-used.")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}

			t.Logf("%s: %d -> %d bytes (%.2f:1) %t (table: %d bytes)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re, len(s.OutTable))
		})
	}
}

func TestCompress1XReuse(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			var s Scratch
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b, re, err := Compress1X(buf0, &s)
			if err != test.err1X {
				t.Errorf("want error %v (%T), got %v (%T)", test.err1X, test.err1X, err, err)
			}
			if err != nil {
				t.Log(test.name, err.Error())
				return
			}
			if b == nil {
				t.Error("got no output")
				return
			}
			firstData := len(s.OutData)
			s.Reuse = ReusePolicyAllow
			b, re, err = Compress1X(buf0, &s)
			if err != nil {
				t.Errorf("got secondary error %v (%T)", err, err)
				return
			}
			if !re {
				t.Error("Didn't re-use even if data was the same")
			}
			if len(s.OutTable) != 0 {
				t.Error("got table definition, don't want any")
			}
			if len(s.OutData) == 0 {
				t.Error("got no data output")
			}
			if len(b) != firstData {
				t.Errorf("data length did not match first: %d, second:%d", firstData, len(b))
			}
			t.Logf("%s: %d -> %d bytes (%.2f:1) %t", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)), re)
		})
	}
}

func BenchmarkDeflate(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			dec, err := flate.NewWriter(ioutil.Discard, flate.HuffmanOnly)
			if err != nil {
				b.Fatal(err)
			}
			if test.err1X != nil {
				b.Skip("skipping")
			}
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				dec.Reset(ioutil.Discard)
				n, err := dec.Write(buf0)
				if err != nil {
					b.Fatal(err)
				}
				if n != len(buf0) {
					b.Fatal("mismatch", n, len(buf0))
				}
				dec.Close()
			}
		})
	}
}

func BenchmarkCompress1XReuseNone(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyNone
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress1X(buf0, &s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress1X(buf0, &s)
				if re {
					b.Fatal("reused")
				}
			}
		})
	}
}

func BenchmarkCompress1XReuseAllow(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyAllow
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress1X(buf0, &s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress1X(buf0, &s)
				if !re {
					b.Fatal("not reused")
				}
			}
		})
	}
}

func BenchmarkCompress1XReusePrefer(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err1X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyPrefer
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress1X(buf0, &s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress1X(buf0, &s)
				if !re {
					b.Fatal("not reused")
				}
			}
		})
	}
}

func BenchmarkCompress4XReuseNone(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyNone
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress4X(buf0, &s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress4X(buf0, &s)
				if re {
					b.Fatal("reused")
				}
			}
		})
	}
}

func BenchmarkCompress4XReuseAllow(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyAllow
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress4X(buf0, &s)
			if err != test.err1X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress4X(buf0, &s)
				if !re {
					b.Fatal("not reused")
				}
			}
		})
	}
}

func BenchmarkCompress4XReusePrefer(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		if test.err4X != nil {
			continue
		}
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			s.Reuse = ReusePolicyPrefer
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			if len(buf0) > BlockSizeMax {
				buf0 = buf0[:BlockSizeMax]
			}
			_, re, err := Compress4X(buf0, &s)
			if err != test.err4X {
				b.Fatal("unexpected error:", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, re, _ = Compress4X(buf0, &s)
				if !re {
					b.Fatal("not reused")
				}
			}
		})
	}
}
