// Copyright 2018 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Based on work Copyright (c) 2013, Yann Collet, released under BSD License.

package fse

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type inputFn func() ([]byte, error)

var testfiles = []struct {
	name string
	fn   inputFn
	err  error
}{
	// gettysburg.txt is a small plain text.
	{name: "gettysburg", fn: func() ([]byte, error) { return os.ReadFile("../testdata/gettysburg.txt") }},
	// Digits is the digits of the irrational number e. Its decimal representation
	// does not repeat, but there are only 10 possible digits, so it should be
	// reasonably compressible.
	{name: "digits", fn: func() ([]byte, error) { return os.ReadFile("../testdata/e.txt") }},
	// Twain is Project Gutenberg's edition of Mark Twain's classic English novel.
	{name: "twain", fn: func() ([]byte, error) { return os.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt") }},
	// Random bytes
	{name: "random", fn: func() ([]byte, error) { return os.ReadFile("../testdata/sharnd.out") }, err: ErrIncompressible},
	// Low entropy
	{name: "low-ent", fn: func() ([]byte, error) { return []byte(strings.Repeat("1221", 10000)), nil }},
	// Super Low entropy
	{name: "superlow-ent", fn: func() ([]byte, error) { return []byte(strings.Repeat("1", 10000) + strings.Repeat("2", 500)), nil }},
	// Zero bytes
	{name: "zeroes", fn: func() ([]byte, error) { return make([]byte, 10000), nil }, err: ErrUseRLE},
	{name: "crash1", fn: func() ([]byte, error) { return os.ReadFile("../testdata/crash1.bin") }, err: ErrIncompressible},
	{name: "crash2", fn: func() ([]byte, error) { return os.ReadFile("../testdata/crash2.bin") }, err: ErrIncompressible},
	{name: "crash3", fn: func() ([]byte, error) { return os.ReadFile("../testdata/crash3.bin") }, err: ErrIncompressible},
	{name: "endzerobits", fn: func() ([]byte, error) { return os.ReadFile("../testdata/endzerobits.bin") }, err: nil},
	{name: "endnonzero", fn: func() ([]byte, error) { return os.ReadFile("../testdata/endnonzero.bin") }, err: ErrIncompressible},
	{name: "case1", fn: func() ([]byte, error) { return os.ReadFile("../testdata/case1.bin") }, err: ErrIncompressible},
	{name: "case2", fn: func() ([]byte, error) { return os.ReadFile("../testdata/case2.bin") }, err: ErrIncompressible},
	{name: "case3", fn: func() ([]byte, error) { return os.ReadFile("../testdata/case3.bin") }, err: ErrIncompressible},
	{name: "pngdata.001", fn: func() ([]byte, error) { return os.ReadFile("../testdata/pngdata.bin") }, err: nil},
	{name: "normcount2", fn: func() ([]byte, error) { return os.ReadFile("../testdata/normcount2.bin") }, err: nil},
}

var decTestfiles = []struct {
	name string
	fn   inputFn
	err  string
}{
	// gettysburg.txt is a small plain text.
	{name: "hang1", fn: func() ([]byte, error) { return os.ReadFile("../testdata/dec-hang1.bin") }, err: "corruption detected (bitCount 252 > 32)"},
	{name: "hang2", fn: func() ([]byte, error) { return os.ReadFile("../testdata/dec-hang2.bin") }, err: "newState (0) == oldState (0) and no bits"},
	{name: "hang3", fn: func() ([]byte, error) { return os.ReadFile("../testdata/dec-hang3.bin") }, err: "maxSymbolValue too small"},
	{name: "symlen1", fn: func() ([]byte, error) { return os.ReadFile("../testdata/dec-symlen1.bin") }, err: "symbolLen (257) too big"},
	{name: "crash4", fn: func() ([]byte, error) { return os.ReadFile("../testdata/crash4.bin") }, err: "corruption detected (count 32768 > 32767)"},
	{name: "crash5", fn: func() ([]byte, error) { return os.ReadFile("../testdata/crash5.bin") }, err: "corruption detected (count 32768 > 32767)"},
	{name: "crash6", fn: func() ([]byte, error) { return os.ReadFile("../testdata/dec-crash6.bin") }, err: "corruption detected (count 32768 > 32767)"},
	{name: "something", fn: func() ([]byte, error) { return os.ReadFile("../testdata/fse-artifact3.bin") }, err: "corrupt stream, did not find end of stream"},
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
			if err != test.err {
				t.Errorf("want error %v (%T), got %v (%T)", test.err, test.err, err, err)
			}
			if b == nil {
				t.Log(test.name + ": not compressible")
				return
			}
			t.Logf("%s: %d -> %d bytes (%.2f:1)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)))
		})
	}
}

func ExampleCompress() {
	// Read data
	data, err := os.ReadFile("../testdata/e.txt")
	if err != nil {
		panic(err)
	}

	// Create re-usable scratch buffer.
	var s Scratch
	b, err := Compress(data, &s)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Compress: %d -> %d bytes (%.2f:1)\n", len(data), len(b), float64(len(data))/float64(len(b)))
	// OUTPUT: Compress: 100003 -> 41564 bytes (2.41:1)
}

func TestDecompress(t *testing.T) {
	for _, test := range decTestfiles {
		t.Run(test.name, func(t *testing.T) {
			var s Scratch
			s.DecompressLimit = 1 << 20
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			b, err := Decompress(buf0, &s)
			if fmt.Sprint(err) != test.err {
				t.Errorf("want error %q, got %q (%T)", test.err, err, err)
				return
			}
			if err != nil {
				return
			}
			if len(b) == 0 {
				t.Error(test.name + ": no output")
				return
			}
			t.Logf("%s: %d -> %d bytes (1:%.2f)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)))
		})
	}
}

// TestDecompressCountOverflow decodes a table whose only count, 1<<15, does
// not fit in an int16, on a Scratch whose previous table filled the decoding
// table. Truncated, the count left that table in place and decoding panicked.
func TestDecompressCountOverflow(t *testing.T) {
	var s Scratch
	s.DecompressLimit = 255
	// tableLog 15, counts 0, -1, 11000, 21767.
	if _, err := Decompress([]byte{0x1a, 0x00, 0x00, 0x00, 0x90, 0xaf, 0xfa, 0xff, 0x03, 0x00, 0x80, 0xff, 0x7f}, &s); err != nil {
		t.Fatal(err)
	}
	// tableLog 15, counts 0, 32768.
	_, err := Decompress([]byte{0x1a, 0x00, 0xe0, 0xff, 0x1f, 0x0f, 0xb1, 0x9c, 0x5a, 0xcf, 0x4d, 0xe4, 0x1d, 0x7b, 0xab, 0x99, 0x5e, 0x01, 0x51, 0xa4, 0x73, 0xd8, 0xb1, 0xf3, 0x39, 0x4b, 0x67, 0xf6}, &s)
	if want := "corruption detected (count 32768 > 32767)"; fmt.Sprint(err) != want {
		t.Fatalf("want error %q, got %v", want, err)
	}
}

func ExampleDecompress() {
	// Read data
	data, err := os.ReadFile("../testdata/e.txt")
	if err != nil {
		panic(err)
	}

	// Create re-usable scratch buffer.
	var s Scratch
	b, err := Compress(data, &s)
	if err != nil {
		panic(err)
	}

	// Since we use the output of compression, it cannot be used as output for decompression.
	s.Out = make([]byte, 0, len(data))
	d, err := Decompress(b, &s)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Input matches: %t\n", bytes.Equal(d, data))
	// OUTPUT: Input matches: true
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
			_, err = Compress(buf0, &s)
			if err != test.err {
				b.Fatal("unexpected error:", err)
			}
			if err != nil {
				b.Skip("skipping benchmark: ", err)
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
		test := testfiles[i]
		t.Run(test.name, func(t *testing.T) {
			name := test.name + ": "
			buf0, err := testfiles[i].fn()
			if err != nil {
				t.Fatal(err)
			}
			b, err := Compress(buf0, &s)
			if err != test.err {
				t.Error(err)
				return
			}
			if err != nil {
				t.Skip(name + err.Error())
				return
			}
			t.Logf("%s: %d -> %d bytes (%.2f:1)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)))
			//t.Logf("%v", b)
			var s2 Scratch
			dc, err := Decompress(b, &s2)
			if err != nil {
				t.Fatal(err)
			}
			want := s.norm[:s.symbolLen]
			got := s2.norm[:s2.symbolLen]
			if !reflect.DeepEqual(want, got) {
				if s.actualTableLog != s2.actualTableLog {
					t.Errorf("%snorm table, want tablelog: %d, got %d", name, s.actualTableLog, s2.actualTableLog)
				}
				if s.symbolLen != s2.symbolLen {
					t.Errorf("%snorm table, want size: %d, got %d", name, s.symbolLen, s2.symbolLen)
				}
				t.Errorf("%snorm table, got delta: \n", name)
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
					t.Errorf("%sdecompressed, want size: %d, got %d", name, len(buf0), len(dc))
					if len(buf0) > len(dc) {
						buf0 = buf0[:len(dc)]
					} else {
						dc = dc[:len(buf0)]
					}
					if !bytes.Equal(buf0, dc) {
						t.Errorf("%sdecompressed, got delta: (in) %v != (out) %v\n", name, buf0, dc)
					}
					return
				}
				if !bytes.Equal(buf0, dc) {
					t.Errorf("%sdecompressed, got delta.", name)
				}
				if !t.Failed() {
					t.Log("... roundtrip ok!")
				}
			}
		})
	}
}

func BenchmarkDecompress(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		b.Run(test.name, func(b *testing.B) {
			var s, s2 Scratch
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			out, err := Compress(buf0, &s)
			if err != test.err {
				b.Fatal(err)
			}
			if err != nil {
				b.Skip(test.name + ": " + err.Error())
				return
			}
			got, err := Decompress(out, &s2)
			if err != nil {
				b.Fatal(err)
			}
			if !bytes.Equal(buf0, got) {
				b.Fatal("output mismatch")
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				_, err = Decompress(out, &s2)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
