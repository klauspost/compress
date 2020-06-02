package zstd

import (
	"bytes"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestDecoder_SmallDict(t *testing.T) {
	// All files have CRC
	fn := "testdata/dict-tests-small.zip"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
		return
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".dict") {
			continue
		}
		func() {
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			err = dec.RegisterDict(in)
			if err != nil {
				t.Fatal(tt.Name, err)
			}
		}()
	}
	defer dec.Close()
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("decodeall-"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			got, err := dec.DecodeAll(in, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = dec.DecodeAll(in, got[:0])
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDecoder_MoreDicts(t *testing.T) {
	// All files have CRC
	// https://files.klauspost.com/compress/zstd-dict-tests.zip
	fn := "testdata/zstd-dict-tests.zip"
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Skip("extended dict test not found.")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
		return
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".dict") {
			continue
		}
		func() {
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			err = dec.RegisterDict(in)
			if err != nil {
				t.Fatal(tt.Name, err)
			}
		}()
	}
	defer dec.Close()
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("decodeall-"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			got, err := dec.DecodeAll(in, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = dec.DecodeAll(in, got[:0])
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
