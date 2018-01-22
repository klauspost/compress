package fse

import (
	"io/ioutil"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type inputFn func() ([]byte, error)

var testfiles = []struct {
	name string
	fn   inputFn
}{
	// gettysburg.txt is a small plain text.
	{name: "gettysburg", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/gettysburg.txt") }},
	// Digits is the digits of the irrational number e. Its decimal representation
	// does not repeat, but there are only 10 possible digits, so it should be
	// reasonably compressible.
	{name: "numbers", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/e.txt") }},
	// Twain is Project Gutenberg's edition of Mark Twain's classic English novel.
	{name: "twain", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt") }},
	// Random bytes
	{name: "random", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/sharnd.out") }},
	// Low entropy
	{name: "low-ent", fn: func() ([]byte, error) { return []byte(strings.Repeat("1221", 10000)), nil }},
	// Super Low entropy
	{name: "superlow-ent", fn: func() ([]byte, error) { return []byte(strings.Repeat("1", 10000) + strings.Repeat("2", 500)), nil }},
	// Zero bytes
	{name: "zeroes", fn: func() ([]byte, error) { return make([]byte, 10000), nil }},
}

func TestCompress(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			var s Scratch
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			b, err := Compress(buf0, &s)
			if err != nil {
				t.Error(err)
			}
			if b == nil {
				t.Log(test.name + ": not compressible")
				return
			}
			t.Logf("%s: %d -> %d bytes (%.2f:1)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)))
		})
	}
}

func BenchmarkCompress(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		b.Run(test.name, func(b *testing.B) {
			var s Scratch
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			out, err := Compress(buf0, &s)
			if err != nil {
				b.Fatal(err)
			}
			if out == nil {
				b.Skip(test.name + ": not compressible")
				return
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, _ = Compress(buf0, &s)
			}
		})
	}
}

func TestReadNCount(t *testing.T) {
	for i := range testfiles {
		var s Scratch
		name := testfiles[i].name
		t.Run(name, func(t *testing.T) {
			name += ": "
			buf0, err := testfiles[i].fn()
			if err != nil {
				t.Fatal(err)
			}
			b, err := Compress(buf0, &s)
			if err != nil {
				t.Error(err)
				return
			}
			if b == nil {
				t.Log(name + "not compressible")
				return
			}
			t.Logf(name+"%d -> %d bytes", len(buf0), len(b))
			//t.Logf("%v", b)
			var s2 Scratch
			dc, err := Decompress(b, &s2)
			if err != nil {
				t.Fatal(err)
			}
			want := s.norm[:s.symbolLen]
			got := s2.norm[:s2.symbolLen]
			if !cmp.Equal(want, got) {
				if s.actualTableLog != s2.actualTableLog {
					t.Errorf(name+"norm table, want tablelog: %d, got %d", s.actualTableLog, s2.actualTableLog)
				}
				if s.symbolLen != s2.symbolLen {
					t.Errorf(name+"norm table, want size: %d, got %d", s.symbolLen, s2.symbolLen)
				}
				t.Errorf(name+"norm table, got delta: \n%s", cmp.Diff(want, got))
				return
			}
			for i, dec := range s2.decTable {
				dd := dec.symbol
				ee := s.ct.tableSymbol[i]
				if dd != ee {
					t.Errorf("table symbol mismatch. idx %d, enc: %v, dec:%v", i, ee, dd)
					break
				}
			}
			if dc != nil {
				if len(buf0) != len(dc) {
					t.Errorf(name+"decompressed, want size: %d, got %d", len(buf0), len(dc))
					if len(buf0) > len(dc) {
						buf0 = buf0[:len(dc)]
					} else {
						dc = dc[:len(buf0)]
					}
					if !cmp.Equal(buf0[:16], dc[:16]) {
						t.Errorf(name+"decompressed, got delta: %v != %v\n", buf0[:16], dc[:16])
					}
					return
				}
				if !cmp.Equal(buf0, dc) {
					t.Errorf(name+"decompressed, got delta: \n%s", cmp.Diff(buf0, dc))
				}
			}
		})
	}
}
