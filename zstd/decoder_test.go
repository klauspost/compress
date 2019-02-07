package zstd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestNewDecoder(t *testing.T) {
	data, err := ioutil.ReadFile("testdata/decoder.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.GOMAXPROCS(2)
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			dec, err := NewDecoder(r)
			if err != nil {
				t.Error(err)
				return
			}
			defer dec.Close()
			got, err := ioutil.ReadAll(dec)
			if err != nil {
				if err == errNotimplemented {
					t.Skip(err)
					return
				}
				t.Error(err)
				return
			}
			fmt.Println(len(got), "bytes returned")
		})
		break
	}
}

func TestNewDecoderGood(t *testing.T) {
	data, err := ioutil.ReadFile("testdata/good.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			dec, err := NewDecoder(r)
			if err != nil {
				if err == errNotimplemented {
					t.Skip(err)
					return
				}
				t.Error(err)
				return
			}
			defer dec.Close()
			got, err := ioutil.ReadAll(dec)
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Println(got)
		})
	}
}
