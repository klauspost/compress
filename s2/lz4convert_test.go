// Copyright (c) 2022 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"sort"
	"testing"

	"github.com/klauspost/compress/internal/lz4ref"
)

func TestLZ4Converter_ConvertBlock(t *testing.T) {
	for _, tf := range testFiles {
		t.Run(tf.label, func(t *testing.T) {
			if err := downloadBenchmarkFiles(t, tf.filename); err != nil {
				t.Fatalf("failed to download testdata: %s", err)
			}

			bDir := filepath.FromSlash(*benchdataDir)
			data := readFile(t, filepath.Join(bDir, tf.filename))
			if n := tf.sizeLimit; 0 < n && n < len(data) {
				data = data[:n]
			}

			lz4Data := make([]byte, lz4ref.CompressBlockBound(len(data)))
			n, err := lz4ref.CompressBlock(data, lz4Data)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Skip("incompressible")
				return
			}
			t.Log("input size:", len(data))
			t.Log("lz4 size:", n)
			lz4Data = lz4Data[:n]
			s2Dst := make([]byte, binary.MaxVarintLen32, MaxEncodedLen(len(data)))
			s2Dst = s2Dst[:binary.PutUvarint(s2Dst, uint64(len(data)))]
			hdr := len(s2Dst)

			conv := LZ4Converter{}

			sz := 0
			out, n, err := conv.ConvertBlock(s2Dst, lz4Data)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(data) {
				t.Fatalf("length mismatch: want %d, got %d", len(data), n)
			}
			sz = len(out) - hdr
			t.Log("lz4->s2 size:", sz)

			decom, err := Decode(nil, out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decom, data) {
				t.Errorf("output mismatch")
			}

			out2 := Encode(s2Dst[:0], data)
			sz2 := len(out2) - hdr
			t.Log("s2 (default) size:", sz2)

			out2 = EncodeBetter(s2Dst[:0], data)
			sz3 := len(out2) - hdr
			t.Log("s2 (better) size:", sz3)

			t.Log("lz4 -> s2 bytes saved:", len(lz4Data)-sz)
			t.Log("data -> s2 (default) bytes saved:", len(lz4Data)-sz2)
			t.Log("data -> s2 (better) bytes saved:", len(lz4Data)-sz3)
			t.Log("direct data -> s2 (default) compared to converted from lz4:", sz-sz2)
			t.Log("direct data -> s2 (better) compared to converted from lz4:", sz-sz3)
		})
	}
}

func BenchmarkLZ4Converter_ConvertBlock(b *testing.B) {
	for _, tf := range testFiles {
		b.Run(tf.label, func(b *testing.B) {
			if err := downloadBenchmarkFiles(b, tf.filename); err != nil {
				b.Fatalf("failed to download testdata: %s", err)
			}

			bDir := filepath.FromSlash(*benchdataDir)
			data := readFile(b, filepath.Join(bDir, tf.filename))
			if n := tf.sizeLimit; 0 < n && n < len(data) {
				data = data[:n]
			}

			lz4Data := make([]byte, lz4ref.CompressBlockBound(len(data)))
			n, err := lz4ref.CompressBlock(data, lz4Data)
			if err != nil {
				b.Fatal(err)
			}
			if n == 0 {
				b.Skip("incompressible")
				return
			}
			lz4Data = lz4Data[:n]
			s2Dst := make([]byte, MaxEncodedLen(len(data)))
			conv := LZ4Converter{}
			b.ReportAllocs()
			b.ResetTimer()
			b.SetBytes(int64(len(data)))
			sz := 0
			for i := 0; i < b.N; i++ {
				out, n, err := conv.ConvertBlock(s2Dst[:0], lz4Data)
				if err != nil {
					b.Fatal(err)
				}
				if n != len(data) {
					b.Fatalf("length mismatch: want %d, got %d", len(data), n)
				}
				sz = len(out)
			}
			b.ReportMetric(float64(len(lz4Data)-sz), "b_saved")
		})
	}
}

func BenchmarkLZ4Converter_ConvertBlockParallel(b *testing.B) {
	sort.Slice(testFiles, func(i, j int) bool {
		return testFiles[i].filename < testFiles[j].filename
	})
	for _, tf := range testFiles {
		b.Run(tf.filename, func(b *testing.B) {
			if err := downloadBenchmarkFiles(b, tf.filename); err != nil {
				b.Fatalf("failed to download testdata: %s", err)
			}

			bDir := filepath.FromSlash(*benchdataDir)
			data := readFile(b, filepath.Join(bDir, tf.filename))

			lz4Data := make([]byte, lz4ref.CompressBlockBound(len(data)))
			n, err := lz4ref.CompressBlock(data, lz4Data)
			if err != nil {
				b.Fatal(err)
			}
			if n == 0 {
				b.Skip("incompressible")
				return
			}
			lz4Data = lz4Data[:n]
			conv := LZ4Converter{}
			b.ReportAllocs()
			b.ResetTimer()
			b.SetBytes(int64(len(data)))
			b.RunParallel(func(pb *testing.PB) {
				s2Dst := make([]byte, MaxEncodedLen(len(data)))
				for pb.Next() {
					_, n, err := conv.ConvertBlock(s2Dst[:0], lz4Data)
					if err != nil {
						b.Fatal(err)
					}
					if n != len(data) {
						b.Fatalf("length mismatch: want %d, got %d", len(data), n)
					}
				}
			})
		})
	}
}
func BenchmarkCompressBlockReference(b *testing.B) {
	b.Skip("Only reference for BenchmarkLZ4Converter_ConvertBlock")
	for _, tf := range testFiles {
		b.Run(tf.label, func(b *testing.B) {
			if err := downloadBenchmarkFiles(b, tf.filename); err != nil {
				b.Fatalf("failed to download testdata: %s", err)
			}
			bDir := filepath.FromSlash(*benchdataDir)
			data := readFile(b, filepath.Join(bDir, tf.filename))
			if n := tf.sizeLimit; 0 < n && n < len(data) {
				data = data[:n]
			}

			lz4Data := make([]byte, lz4ref.CompressBlockBound(len(data)))
			n, err := lz4ref.CompressBlock(data, lz4Data)
			if err != nil {
				b.Fatal(err)
			}
			if n == 0 {
				b.Skip("incompressible")
				return
			}
			s2Dst := make([]byte, MaxEncodedLen(len(data)))

			b.Run("default", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				b.SetBytes(int64(len(data)))
				for i := 0; i < b.N; i++ {
					_ = Encode(s2Dst, data)
				}
			})
			b.Run("better", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				b.SetBytes(int64(len(data)))
				for i := 0; i < b.N; i++ {
					_ = EncodeBetter(s2Dst, data)
				}
			})
		})
	}
}
