// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd/internal/xxhash"
)

func TestNewReaderMismatch(t *testing.T) {
	// To identify a potential decoding error, do the following steps:
	// 1) Place the compressed file in testdata, eg 'testdata/backup.bin.zst'
	// 2) Decompress the file to using zstd, so it will be named 'testdata/backup.bin'
	// 3) Run the test. A hash file will be generated 'testdata/backup.bin.hash'
	// 4) The decoder will also run and decode the file. It will stop as soon as a mismatch is found.
	// The hash file will be reused between runs if present.
	const baseFile = "testdata/backup.bin"
	const blockSize = 1024
	hashes, err := ioutil.ReadFile(baseFile + ".hash")
	if os.IsNotExist(err) {
		// Create the hash file.
		f, err := os.Open(baseFile)
		if os.IsNotExist(err) {
			t.Skip("no decompressed file found")
			return
		}
		defer f.Close()
		br := bufio.NewReader(f)
		var tmp [8]byte
		xx := xxhash.New()
		for {
			xx.Reset()
			buf := make([]byte, blockSize)
			n, err := io.ReadFull(br, buf)
			if err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					t.Fatal(err)
				}
			}
			if n > 0 {
				_, _ = xx.Write(buf[:n])
				binary.LittleEndian.PutUint64(tmp[:], xx.Sum64())
				hashes = append(hashes, tmp[4:]...)
			}
			if n != blockSize {
				break
			}
		}
		err = ioutil.WriteFile(baseFile+".hash", hashes, os.ModePerm)
		if err != nil {
			// We can continue for now
			t.Error(err)
		}
		t.Log("Saved", len(hashes)/4, "hashes as", baseFile+".hash")
	}

	f, err := os.Open(baseFile + ".zst")
	if os.IsNotExist(err) {
		t.Skip("no compressed file found")
		return
	}
	defer f.Close()
	dec, err := NewReader(f, WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	var tmp [8]byte
	xx := xxhash.New()
	var cHash int
	for {
		xx.Reset()
		buf := make([]byte, blockSize)
		n, err := io.ReadFull(dec, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				t.Fatal(err)
			}
		}
		if n > 0 {
			if cHash+4 > len(hashes) {
				extra, _ := io.Copy(ioutil.Discard, dec)
				t.Fatal("not enough hashes (length mismatch). Only have", len(hashes)/4, "hashes. Got block of", n, "bytes and", extra, "bytes still on stream.")
			}
			_, _ = xx.Write(buf[:n])
			binary.LittleEndian.PutUint64(tmp[:], xx.Sum64())
			want, got := hashes[cHash:cHash+4], tmp[4:]
			if !bytes.Equal(want, got) {
				org, err := os.Open(baseFile)
				if err == nil {
					const sizeBack = 8 << 20
					defer org.Close()
					start := int64(cHash)/4*blockSize - sizeBack
					if start < 0 {
						start = 0
					}
					_, err = org.Seek(start, io.SeekStart)
					buf2 := make([]byte, sizeBack+1<<20)
					n, _ := io.ReadFull(org, buf2)
					if n > 0 {
						err = ioutil.WriteFile(baseFile+".section", buf2[:n], os.ModePerm)
						if err == nil {
							t.Log("Wrote problematic section to", baseFile+".section")
						}
					}
				}

				t.Fatal("block", cHash/4, "offset", cHash/4*blockSize, "hash mismatch, want:", hex.EncodeToString(want), "got:", hex.EncodeToString(got))
			}
			cHash += 4
		}
		if n != blockSize {
			break
		}
	}
	t.Log("Output matched")
}

func TestNewDecoder(t *testing.T) {
	defer timeout(60 * time.Second)()
	testDecoderFile(t, "testdata/decoder.zip")
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, "testdata/decoder.zip", dec)
}

func TestNewDecoderGood(t *testing.T) {
	defer timeout(30 * time.Second)()
	testDecoderFile(t, "testdata/good.zip")
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, "testdata/good.zip", dec)
}

func TestNewDecoderBad(t *testing.T) {
	defer timeout(10 * time.Second)()
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAllError(t, "testdata/bad.zip", dec)
}

func TestNewDecoderLarge(t *testing.T) {
	testDecoderFile(t, "testdata/large.zip")
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, "testdata/large.zip", dec)
}

func TestNewReaderRead(t *testing.T) {
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dec.Read([]byte{0})
	if err == nil {
		t.Fatal("Wanted error on uninitialized read, got nil")
	}
	t.Log("correctly got error", err)
}

func TestNewDecoderBig(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/zstd-10kfiles.zip"
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Skip("To run extended tests, download https://files.klauspost.com/compress/zstd-10kfiles.zip \n" +
			"and place it in " + file + "\n" + "Running it requires about 5GB of RAM")
	}
	testDecoderFile(t, file)
	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, file, dec)
}

func TestNewDecoderBigFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/enwik9.zst"
	const wantSize = 1000000000
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Skip("To run extended tests, download http://mattmahoney.net/dc/enwik9.zip unzip it \n" +
			"compress it with 'zstd -15 -T0 enwik9' and place it in " + file)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		t.Fatal(err)
	}
	if n != wantSize {
		t.Errorf("want size %d, got size %d", wantSize, n)
	}
	elapsed := time.Since(start)
	mbpersec := (float64(n) / (1024 * 1024)) / (float64(elapsed) / (float64(time.Second)))
	t.Logf("Decoded %d bytes with %f.2 MB/s", n, mbpersec)
}

func TestNewDecoderSmallFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/z000028.zst"
	const wantSize = 39807
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		t.Fatal(err)
	}
	if n != wantSize {
		t.Errorf("want size %d, got size %d", wantSize, n)
	}
	mbpersec := (float64(n) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Decoded %d bytes with %f.2 MB/s", n, mbpersec)
}

func TestDecoderRegression(t *testing.T) {
	defer timeout(5 * time.Second)()
	data, err := ioutil.ReadFile("testdata/regression.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderLowmem(true), WithDecoderMaxMemory(10<<20))
	if err != nil {
		t.Error(err)
		return
	}
	defer dec.Close()

	for _, tt := range zr.File {
		if !strings.HasSuffix(t.Name(), "") {
			continue
		}
		t.Run("Reader-"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			err = dec.Reset(r)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := ioutil.ReadAll(dec)
			t.Log("Received:", len(got), err)
		})
		t.Run("DecodeAll-"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Error(err)
			}
			got, err := dec.DecodeAll(in, nil)
			t.Log("Received:", len(got), err)
		})
	}
}

func TestDecoder_Reset(t *testing.T) {
	in, err := ioutil.ReadFile("testdata/z000028")
	if err != nil {
		t.Fatal(err)
	}
	in = append(in, in...)
	var e Encoder
	start := time.Now()
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

	dec, err := NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")

	// Decode using reset+copy
	for i := 0; i < 3; i++ {
		err = dec.Reset(bytes.NewBuffer(dst))
		if err != nil {
			t.Fatal(err)
		}
		var dBuf bytes.Buffer
		n, err := io.Copy(&dBuf, dec)
		if err != nil {
			t.Fatal(err)
		}
		decoded = dBuf.Bytes()
		if int(n) != len(decoded) {
			t.Fatalf("decoded reported length mismatch %d != %d", n, len(decoded))
		}
		if !bytes.Equal(decoded, in) {
			ioutil.WriteFile("testdata/"+t.Name()+"-z000028.got", decoded, os.ModePerm)
			ioutil.WriteFile("testdata/"+t.Name()+"-z000028.want", in, os.ModePerm)
			t.Fatal("Decoded does not match")
		}
	}
	// Test without WriterTo interface support.
	for i := 0; i < 3; i++ {
		err = dec.Reset(bytes.NewBuffer(dst))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := ioutil.ReadAll(ioutil.NopCloser(dec))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, in) {
			ioutil.WriteFile("testdata/"+t.Name()+"-z000028.got", decoded, os.ModePerm)
			ioutil.WriteFile("testdata/"+t.Name()+"-z000028.want", in, os.ModePerm)
			t.Fatal("Decoded does not match")
		}
	}
}

func testDecoderFile(t *testing.T, fn string) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var want = make(map[string][]byte)
	for _, tt := range zr.File {
		if strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
			return
		}
		want[tt.Name+".zst"], _ = ioutil.ReadAll(r)
	}

	dec, err := NewReader(nil)
	if err != nil {
		t.Error(err)
		return
	}
	defer dec.Close()
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("Reader-"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			defer r.Close()
			err = dec.Reset(r)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := ioutil.ReadAll(dec)
			if err != nil {
				t.Error(err)
				if err != ErrCRCMismatch {
					return
				}
			}
			wantB := want[tt.Name]
			if !bytes.Equal(wantB, got) {
				if len(wantB)+len(got) < 1000 {
					t.Logf(" got: %v\nwant: %v", got, wantB)
				} else {
					fileName, _ := filepath.Abs(filepath.Join("testdata", t.Name()+"-want.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err := ioutil.WriteFile(fileName, wantB, os.ModePerm)
					t.Log("Wrote file", fileName, err)

					fileName, _ = filepath.Abs(filepath.Join("testdata", t.Name()+"-got.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err = ioutil.WriteFile(fileName, got, os.ModePerm)
					t.Log("Wrote file", fileName, err)
				}
				t.Logf("Length, want: %d, got: %d", len(wantB), len(got))
				t.Error("Output mismatch")
				return
			}
			t.Log(len(got), "bytes returned, matches input, ok!")
		})
	}
}

func BenchmarkDecoder_DecodeAll(b *testing.B) {
	fn := "testdata/decoder.zip"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		b.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}
	dec, err := NewReader(nil)
	if err != nil {
		b.Fatal(err)
		return
	}
	defer dec.Close()
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		if strings.HasSuffix(tt.Name, "0010.zst") {
			break
		}
		b.Run(tt.Name, func(b *testing.B) {
			r, err := tt.Open()
			if err != nil {
				b.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				b.Fatal(err)
			}
			got, err := dec.DecodeAll(in, nil)
			b.SetBytes(int64(len(got)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err = dec.DecodeAll(in, got[:0])
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

/*
func BenchmarkDecoder_DecodeAllCgo(b *testing.B) {
	fn := "testdata/decoder.zip"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		b.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		if strings.HasSuffix(tt.Name, "0010.zst") {
			break
		}
		b.Run(tt.Name, func(b *testing.B) {
			tt := tt
			r, err := tt.Open()
			if err != nil {
				b.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				b.Fatal(err)
			}
			got, err := zstd.Decompress(nil, in)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(got)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err = zstd.Decompress(got[:0], in)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecoderSilesiaCgo(b *testing.B) {
	fn := "testdata/silesia.tar.zst"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		if os.IsNotExist(err) {
			b.Skip("Missing testdata/silesia.tar.zst")
			return
		}
		b.Fatal(err)
	}
	dec := zstd.NewReader(bytes.NewBuffer(data))
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := zstd.NewReader(bytes.NewBuffer(data))
		_, err := io.CopyN(ioutil.Discard, dec, n)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkDecoderEnwik9Cgo(b *testing.B) {
	fn := "testdata/enwik9-1.zst"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		if os.IsNotExist(err) {
			b.Skip("Missing " + fn)
			return
		}
		b.Fatal(err)
	}
	dec := zstd.NewReader(bytes.NewBuffer(data))
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec := zstd.NewReader(bytes.NewBuffer(data))
		_, err := io.CopyN(ioutil.Discard, dec, n)
		if err != nil {
			b.Fatal(err)
		}
	}
}

*/

func BenchmarkDecoderSilesia(b *testing.B) {
	fn := "testdata/silesia.tar.zst"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		if os.IsNotExist(err) {
			b.Skip("Missing testdata/silesia.tar.zst")
			return
		}
		b.Fatal(err)
	}
	dec, err := NewReader(nil, WithDecoderLowmem(false))
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()
	err = dec.Reset(bytes.NewBuffer(data))
	if err != nil {
		b.Fatal(err)
	}
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = dec.Reset(bytes.NewBuffer(data))
		if err != nil {
			b.Fatal(err)
		}
		_, err := io.CopyN(ioutil.Discard, dec, n)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecoderEnwik9(b *testing.B) {
	fn := "testdata/enwik9-1.zst"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		if os.IsNotExist(err) {
			b.Skip("Missing " + fn)
			return
		}
		b.Fatal(err)
	}
	dec, err := NewReader(nil, WithDecoderLowmem(false))
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()
	err = dec.Reset(bytes.NewBuffer(data))
	if err != nil {
		b.Fatal(err)
	}
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err = dec.Reset(bytes.NewBuffer(data))
		if err != nil {
			b.Fatal(err)
		}
		_, err := io.CopyN(ioutil.Discard, dec, n)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func testDecoderDecodeAll(t *testing.T, fn string, dec *Decoder) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var want = make(map[string][]byte)
	for _, tt := range zr.File {
		if strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
			return
		}
		want[tt.Name+".zst"], _ = ioutil.ReadAll(r)
	}

	for _, tt := range zr.File {
		tt := tt
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("DecodeAll-"+tt.Name, func(t *testing.T) {
			t.Parallel()
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			wantB := want[tt.Name]
			// make a buffer that is too small.
			got, err := dec.DecodeAll(in, make([]byte, 10, 200))
			if err != nil {
				t.Error(err)
			}
			got = got[10:]
			if !bytes.Equal(wantB, got) {
				if len(wantB)+len(got) < 1000 {
					t.Logf(" got: %v\nwant: %v", got, wantB)
				} else {
					fileName, _ := filepath.Abs(filepath.Join("testdata", t.Name()+"-want.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err := ioutil.WriteFile(fileName, wantB, os.ModePerm)
					t.Log("Wrote file", fileName, err)

					fileName, _ = filepath.Abs(filepath.Join("testdata", t.Name()+"-got.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err = ioutil.WriteFile(fileName, got, os.ModePerm)
					t.Log("Wrote file", fileName, err)
				}
				t.Logf("Length, want: %d, got: %d", len(wantB), len(got))
				t.Error("Output mismatch")
				return
			}
			t.Log(len(got), "bytes returned, matches input, ok!")
		})
	}
}

func testDecoderDecodeAllError(t *testing.T, fn string, dec *Decoder) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range zr.File {
		tt := tt
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("DecodeAll-"+tt.Name, func(t *testing.T) {
			t.Parallel()
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			// make a buffer that is too small.
			_, err = dec.DecodeAll(in, make([]byte, 0, 200))
			if err == nil {
				t.Error("Did not get expected error")
			}
		})
	}
}

// Test our predefined tables are correct.
// We don't predefine them, since this also tests our transformations.
// Reference from here: https://github.com/facebook/zstd/blob/ededcfca57366461021c922720878c81a5854a0a/lib/decompress/zstd_decompress_block.c#L234
func TestPredefTables(t *testing.T) {
	for i := range fsePredef[:] {
		var want []decSymbol
		switch tableIndex(i) {
		case tableLiteralLengths:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 4, 0}, {16, 0, 4, 0},
				{32, 0, 5, 1}, {0, 0, 5, 3},
				{0, 0, 5, 4}, {0, 0, 5, 6},
				{0, 0, 5, 7}, {0, 0, 5, 9},
				{0, 0, 5, 10}, {0, 0, 5, 12},
				{0, 0, 6, 14}, {0, 1, 5, 16},
				{0, 1, 5, 20}, {0, 1, 5, 22},
				{0, 2, 5, 28}, {0, 3, 5, 32},
				{0, 4, 5, 48}, {32, 6, 5, 64},
				{0, 7, 5, 128}, {0, 8, 6, 256},
				{0, 10, 6, 1024}, {0, 12, 6, 4096},
				{32, 0, 4, 0}, {0, 0, 4, 1},
				{0, 0, 5, 2}, {32, 0, 5, 4},
				{0, 0, 5, 5}, {32, 0, 5, 7},
				{0, 0, 5, 8}, {32, 0, 5, 10},
				{0, 0, 5, 11}, {0, 0, 6, 13},
				{32, 1, 5, 16}, {0, 1, 5, 18},
				{32, 1, 5, 22}, {0, 2, 5, 24},
				{32, 3, 5, 32}, {0, 3, 5, 40},
				{0, 6, 4, 64}, {16, 6, 4, 64},
				{32, 7, 5, 128}, {0, 9, 6, 512},
				{0, 11, 6, 2048}, {48, 0, 4, 0},
				{16, 0, 4, 1}, {32, 0, 5, 2},
				{32, 0, 5, 3}, {32, 0, 5, 5},
				{32, 0, 5, 6}, {32, 0, 5, 8},
				{32, 0, 5, 9}, {32, 0, 5, 11},
				{32, 0, 5, 12}, {0, 0, 6, 15},
				{32, 1, 5, 18}, {32, 1, 5, 20},
				{32, 2, 5, 24}, {32, 2, 5, 28},
				{32, 3, 5, 40}, {32, 4, 5, 48},
				{0, 16, 6, 65536}, {0, 15, 6, 32768},
				{0, 14, 6, 16384}, {0, 13, 6, 8192}}
		case tableOffsets:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 5, 0}, {0, 6, 4, 61},
				{0, 9, 5, 509}, {0, 15, 5, 32765},
				{0, 21, 5, 2097149}, {0, 3, 5, 5},
				{0, 7, 4, 125}, {0, 12, 5, 4093},
				{0, 18, 5, 262141}, {0, 23, 5, 8388605},
				{0, 5, 5, 29}, {0, 8, 4, 253},
				{0, 14, 5, 16381}, {0, 20, 5, 1048573},
				{0, 2, 5, 1}, {16, 7, 4, 125},
				{0, 11, 5, 2045}, {0, 17, 5, 131069},
				{0, 22, 5, 4194301}, {0, 4, 5, 13},
				{16, 8, 4, 253}, {0, 13, 5, 8189},
				{0, 19, 5, 524285}, {0, 1, 5, 1},
				{16, 6, 4, 61}, {0, 10, 5, 1021},
				{0, 16, 5, 65533}, {0, 28, 5, 268435453},
				{0, 27, 5, 134217725}, {0, 26, 5, 67108861},
				{0, 25, 5, 33554429}, {0, 24, 5, 16777213}}
		case tableMatchLengths:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 6, 3}, {0, 0, 4, 4},
				{32, 0, 5, 5}, {0, 0, 5, 6},
				{0, 0, 5, 8}, {0, 0, 5, 9},
				{0, 0, 5, 11}, {0, 0, 6, 13},
				{0, 0, 6, 16}, {0, 0, 6, 19},
				{0, 0, 6, 22}, {0, 0, 6, 25},
				{0, 0, 6, 28}, {0, 0, 6, 31},
				{0, 0, 6, 34}, {0, 1, 6, 37},
				{0, 1, 6, 41}, {0, 2, 6, 47},
				{0, 3, 6, 59}, {0, 4, 6, 83},
				{0, 7, 6, 131}, {0, 9, 6, 515},
				{16, 0, 4, 4}, {0, 0, 4, 5},
				{32, 0, 5, 6}, {0, 0, 5, 7},
				{32, 0, 5, 9}, {0, 0, 5, 10},
				{0, 0, 6, 12}, {0, 0, 6, 15},
				{0, 0, 6, 18}, {0, 0, 6, 21},
				{0, 0, 6, 24}, {0, 0, 6, 27},
				{0, 0, 6, 30}, {0, 0, 6, 33},
				{0, 1, 6, 35}, {0, 1, 6, 39},
				{0, 2, 6, 43}, {0, 3, 6, 51},
				{0, 4, 6, 67}, {0, 5, 6, 99},
				{0, 8, 6, 259}, {32, 0, 4, 4},
				{48, 0, 4, 4}, {16, 0, 4, 5},
				{32, 0, 5, 7}, {32, 0, 5, 8},
				{32, 0, 5, 10}, {32, 0, 5, 11},
				{0, 0, 6, 14}, {0, 0, 6, 17},
				{0, 0, 6, 20}, {0, 0, 6, 23},
				{0, 0, 6, 26}, {0, 0, 6, 29},
				{0, 0, 6, 32}, {0, 16, 6, 65539},
				{0, 15, 6, 32771}, {0, 14, 6, 16387},
				{0, 13, 6, 8195}, {0, 12, 6, 4099},
				{0, 11, 6, 2051}, {0, 10, 6, 1027},
			}
		}
		pre := fsePredef[i]
		got := pre.dt[:1<<pre.actualTableLog]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Predefined table %d incorrect, len(got) = %d, len(want) = %d", i, len(got), len(want))
		}
	}
}

func timeout(after time.Duration) (cancel func()) {
	c := time.After(after)
	cc := make(chan struct{})
	go func() {
		select {
		case <-cc:
			return
		case <-c:
			buf := make([]byte, 1<<20)
			stacklen := runtime.Stack(buf, true)
			log.Printf("=== Timeout, assuming deadlock ===\n*** goroutine dump...\n%s\n*** end\n", string(buf[:stacklen]))
			os.Exit(2)
		}
	}()
	return func() {
		close(cc)
	}
}
