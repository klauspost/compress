// Copyright (c) 2023+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s2

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math/rand"
	"os"
	"testing"

	"github.com/klauspost/compress/internal/fuzz"
	"github.com/klauspost/compress/zstd"
)

func TestDict(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 128<<10)
	for i := range data {
		data[i] = uint8(rng.Intn(256))
	}

	// Should match the first 64K
	d := NewDict(append([]byte{0}, data[:65536]...))
	encoded := make([]byte, MaxEncodedLen(len(data)))
	res := encodeBlockDictGo(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded := make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// Add dict that will produce a full match 5000 chars into the input.
	d = NewDict(append([]byte{0}, data[5000:65536+5000]...))
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockDictGo(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// generate copies
	for i := 1; i < len(data); {
		n := rng.Intn(32) + 4
		off := rng.Intn(len(data) - n)
		copy(data[i:], data[off:off+n])
		i += n
	}

	dict := make([]byte, 65536)
	for i := 1; i < len(dict); {
		n := rng.Intn(32) + 4
		off := rng.Intn(65536 - n)
		copy(dict[i:], data[off:off+n])
		i += n
	}
	d = NewDict(dict)
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockDictGo(encoded, data, d)
	if res == 0 || res > len(data)-20000 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		os.WriteFile("decoded.bin", decoded, os.ModePerm)
		os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}
}

func TestDictBetter(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 128<<10)
	for i := range data {
		data[i] = uint8(rng.Intn(256))
	}

	// Should match the first 64K
	d := NewDict(append([]byte{0}, data[:65536]...))
	encoded := make([]byte, MaxEncodedLen(len(data)))
	res := encodeBlockBetterDict(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded := make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// Add dict that will produce a full match 5000 chars into the input.
	d = NewDict(append([]byte{0}, data[5000:65536+5000]...))
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockBetterDict(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// generate copies
	for i := 1; i < len(data); {
		n := rng.Intn(32) + 4
		off := rng.Intn(len(data) - n)
		copy(data[i:], data[off:off+n])
		i += n
	}

	dict := make([]byte, 65536)
	for i := 1; i < len(dict); {
		n := rng.Intn(32) + 4
		off := rng.Intn(65536 - n)
		copy(dict[i:], data[off:off+n])
		i += n
	}
	d = NewDict(dict)
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockBetterDict(encoded, data, d)
	if res == 0 || res > len(data)-20000 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		os.WriteFile("decoded.bin", decoded, os.ModePerm)
		os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}
}

func TestDictBest(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	data := make([]byte, 128<<10)
	for i := range data {
		data[i] = uint8(rng.Intn(256))
	}

	// Should match the first 64K
	d := NewDict(append([]byte{0}, data[:65536]...))
	encoded := make([]byte, MaxEncodedLen(len(data)))
	res := encodeBlockBest(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded := make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// Add dict that will produce a full match 5000 chars into the input.
	d = NewDict(append([]byte{0}, data[5000:65536+5000]...))
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockBest(encoded, data, d)
	if res == 0 || res > len(data)-65500 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}

	// generate copies
	for i := 1; i < len(data); {
		n := rng.Intn(32) + 4
		off := rng.Intn(len(data) - n)
		copy(data[i:], data[off:off+n])
		i += n
	}

	dict := make([]byte, 65536)
	for i := 1; i < len(dict); {
		n := rng.Intn(32) + 4
		off := rng.Intn(65536 - n)
		copy(dict[i:], data[off:off+n])
		i += n
	}
	d = NewDict(dict)
	encoded = make([]byte, MaxEncodedLen(len(data)))
	res = encodeBlockBest(encoded, data, d)
	if res == 0 || res > len(data)-20000 {
		t.Errorf("did no get expected dict saving. Saved %d bytes", len(data)-res)
	}
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	decoded = make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		os.WriteFile("decoded.bin", decoded, os.ModePerm)
		os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}
}

func TestDictBetter2(t *testing.T) {
	// Should match the first 64K
	data := []byte("10 bananas which were brown were added")
	d := NewDict(append([]byte{6}, []byte("Yesterday 25 bananas were added to Benjamins brown bag")...))
	encoded := make([]byte, MaxEncodedLen(len(data)))
	res := encodeBlockBetterDict(encoded, data, d)
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	t.Log(string(encoded))
	decoded := make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}
}

func TestDictBest2(t *testing.T) {
	// Should match the first 64K
	data := []byte("10 bananas which were brown were added")
	d := NewDict(append([]byte{6}, []byte("Yesterday 25 bananas were added to Benjamins brown bag")...))
	encoded := make([]byte, MaxEncodedLen(len(data)))
	res := encodeBlockBest(encoded, data, d)
	encoded = encoded[:res]
	t.Log("saved", len(data)-res, "bytes")
	t.Log(string(encoded))
	decoded := make([]byte, len(data))
	res = s2DecodeDict(decoded, encoded, d)
	if res != 0 {
		t.Fatalf("got result: %d", res)
	}
	if !bytes.Equal(decoded, data) {
		//os.WriteFile("decoded.bin", decoded, os.ModePerm)
		//os.WriteFile("original.bin", data, os.ModePerm)
		t.Fatal("decoded mismatch")
	}
}

func TestDictSize(t *testing.T) {
	//f, err := os.Open("testdata/xlmeta.tar.s2")
	//f, err := os.Open("testdata/broken.tar.s2")
	f, err := os.Open("testdata/github_users_sample_set.tar.s2")
	//f, err := os.Open("testdata/gofiles2.tar.s2")
	//f, err := os.Open("testdata/gosrc.tar.s2")
	if err != nil {
		t.Skip(err)
	}
	stream := NewReader(f)
	in := tar.NewReader(stream)
	//rawDict, err := os.ReadFile("testdata/godict.dictator")
	rawDict, err := os.ReadFile("testdata/gofiles.dict")
	//rawDict, err := os.ReadFile("testdata/gosrc2.dict")
	//rawDict, err := os.ReadFile("testdata/td.dict")
	//rawDict, err := os.ReadFile("testdata/users.dict")
	//rawDict, err := os.ReadFile("testdata/xlmeta.dict")
	if err != nil {
		t.Fatal(err)
	}

	lidx := -1
	if di, err := zstd.InspectDictionary(rawDict); err == nil {
		rawDict = di.Content()
		lidx = len(rawDict) - di.Offsets()[0]
	} else {
		t.Errorf("Loading dict: %v", err)
		return
	}

	searchFor := ""
	if false {
		searchFor = "// Copyright 2022"
	}
	d := MakeDict(rawDict, []byte(searchFor))
	if d == nil {
		t.Fatal("no dict", lidx)
	}

	var totalIn int
	var totalOut int
	var totalCount int
	for {
		h, err := in.Next()
		if err != nil {
			break
		}
		if h.Size == 0 {
			continue
		}
		data := make([]byte, 65536)
		t.Run(h.Name, func(t *testing.T) {
			if int(h.Size) < 65536 {
				data = data[:h.Size]
			} else {
				data = data[:65536]
			}
			_, err := io.ReadFull(in, data)
			if err != nil {
				t.Skip()
			}
			if d == nil {
				// Use first file as dict
				d = MakeDict(data, nil)
			}
			// encode
			encoded := make([]byte, MaxEncodedLen(len(data)))
			totalIn += len(data)
			totalCount++
			//res := encodeBlockBest(encoded, data, nil)
			res := encodeBlockBest(encoded, data, d)
			//res := encodeBlockBetterDict(encoded, data, d)
			//res := encodeBlockBetterGo(encoded, data)
			//res := encodeBlockDictGo(encoded, data, d)
			//			res := encodeBlockGo(encoded, data)
			if res == 0 {
				totalOut += len(data)
				return
			}
			totalOut += res
			encoded = encoded[:res]
			//t.Log("encoded", len(data), "->", res, "saved", len(data)-res, "bytes")
			decoded := make([]byte, len(data))
			res = s2DecodeDict(decoded, encoded, d)
			if res != 0 {
				t.Fatalf("got result: %d", res)
			}
			if !bytes.Equal(decoded, data) {
				os.WriteFile("decoded.bin", decoded, os.ModePerm)
				os.WriteFile("original.bin", data, os.ModePerm)
				t.Fatal("decoded mismatch")
			}
		})
	}
	fmt.Printf("%d files, %d -> %d (%.2f%%) - %.02f bytes saved/file\n", totalCount, totalIn, totalOut, float64(totalOut*100)/float64(totalIn), float64(totalIn-totalOut)/float64(totalCount))
}

func FuzzDictBlocks(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/enc_regressions.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/fuzz/block-corpus-raw.zip", fuzz.TypeRaw, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/block-corpus-enc.zip", fuzz.TypeGoFuzz, testing.Short())

	// Fuzzing tweaks:
	const (
		// Max input size:
		maxSize = 8 << 20
	)
	file, err := os.Open("testdata/s2-dict.bin.gz")
	if err != nil {
		f.Fatal(err)
	}
	gzr, err := gzip.NewReader(file)
	if err != nil {
		f.Fatal(err)
	}
	dictBytes, err := io.ReadAll(gzr)
	if err != nil {
		f.Fatal(err)
	}
	dict := NewDict(dictBytes)
	if dict == nil {
		f.Fatal("invalid dict")
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSize {
			return
		}

		writeDst := make([]byte, MaxEncodedLen(len(data)), MaxEncodedLen(len(data))+4)
		writeDst = append(writeDst, 1, 2, 3, 4)
		defer func() {
			got := writeDst[MaxEncodedLen(len(data)):]
			want := []byte{1, 2, 3, 4}
			if !bytes.Equal(got, want) {
				t.Fatalf("want %v, got %v - dest modified outside cap", want, got)
			}
		}()
		compDst := writeDst[:MaxEncodedLen(len(data)):MaxEncodedLen(len(data))] // Hard cap
		decDst := make([]byte, len(data))
		comp := dict.Encode(compDst, data)
		decoded, err := dict.Decode(decDst, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}
		comp = dict.EncodeBetter(compDst, data)
		decoded, err = dict.Decode(decDst, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}

		comp = dict.EncodeBest(compDst, data)
		decoded, err = dict.Decode(decDst, comp)
		if err != nil {
			t.Error(err)
			return
		}
		if !bytes.Equal(data, decoded) {
			t.Error("block decoder mismatch")
			return
		}
		if mel := MaxEncodedLen(len(data)); len(comp) > mel {
			t.Error(fmt.Errorf("MaxEncodedLen Exceed: input: %d, mel: %d, got %d", len(data), mel, len(comp)))
			return
		}
	})
}
