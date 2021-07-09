// +build gofuzzbeta

package zstd_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func FuzzCompress(f *testing.F) {
	var dec *zstd.Decoder
	var encs [zstd.SpeedBestCompression + 1]*zstd.Encoder
	var encsD [zstd.SpeedBestCompression + 1]*zstd.Encoder

	runtime.GOMAXPROCS(2)
	// Also tests with dictionaries...
	const testDicts = false

	initEnc := func() func() {
		dict, err := ioutil.ReadFile("testdata/d0.dict")
		if err != nil {
			panic(err)
		}
		dec, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderDicts(dict))
		if err != nil {
			panic(err)
		}
		for level := zstd.SpeedFastest; level <= zstd.SpeedBestCompression; level++ {
			encs[level], err = zstd.NewWriter(nil, zstd.WithEncoderCRC(true), zstd.WithEncoderLevel(level), zstd.WithEncoderConcurrency(2), zstd.WithWindowSize(128<<10), zstd.WithZeroFrames(true), zstd.WithLowerEncoderMem(true))
			if testDicts {
				encsD[level], err = zstd.NewWriter(nil, zstd.WithEncoderCRC(true), zstd.WithEncoderLevel(level), zstd.WithEncoderConcurrency(2), zstd.WithWindowSize(128<<10), zstd.WithZeroFrames(true), zstd.WithEncoderDict(dict), zstd.WithLowerEncoderMem(true), zstd.WithLowerEncoderMem(true))
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
	// Run test against out decoder
	var dst bytes.Buffer

	// Create a buffer that will usually be too small.
	corpus, err := os.Open("testdata/corpus.tar.zst")
	if err != nil {
		f.Fatal(err)
	}
	err = dec.Reset(corpus)
	if err != nil {
		f.Fatal(err)
	}
	tr := tar.NewReader(dec)
load_corpus:
	for {
		header, err := tr.Next()
		switch err {
		case io.EOF:
			break load_corpus
		default:
			f.Fatal(err)
		case nil:
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		tmp := make([]byte, header.Size)
		_, err = io.ReadFull(tr, tmp)
		if err != nil {
			f.Fatal(err)
		}
		if header.Size > 100<<10 {
			continue
		}
		f.Add(tmp)
	}
	corpus.Close()
	dec.Reset(nil)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		var bufSize = len(data)
		if bufSize > 2 {
			// Make deterministic size
			bufSize = int(data[0]) | int(data[1])<<8
			if bufSize >= len(data) {
				bufSize = len(data) / 2
			}
		}

		for level := zstd.SpeedFastest; level <= zstd.SpeedBestCompression; level++ {
			enc := encs[level]
			dst.Reset()
			enc.Reset(&dst)
			n, err := enc.Write(data)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(data) {
				t.Fatal(fmt.Sprintln("Level", level, "Short write, got:", n, "want:", len(data)))
			}

			encoded := enc.EncodeAll(data, make([]byte, 0, bufSize))
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
			n, err = enc.Write(data)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(data) {
				t.Fatal(fmt.Sprintln("Dict Level", level, "Short write, got:", n, "want:", len(data)))
			}

			encoded = enc.EncodeAll(data, encoded[:0])
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
