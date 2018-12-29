package zstd

import (
	"bytes"
	"io"
	"io/ioutil"
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
			for {
				got, err := NewDecoder(r)
				if err != nil {
					t.Error(err)
					return
				}
				t.Logf("%+v", *got)
				for {
					b, err := got.frame.next()
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("%+v", b)
					if b.Last {
						break
					}
				}
				if _, err := r.Read(nil); err == io.EOF {
					break
				}
			}
		})
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
			for {
				got, err := NewDecoder(r)
				if err != nil {
					t.Error(err)
					return
				}
				t.Logf("%+v", *got)
				for {
					b, err := got.frame.next()
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("%+v", b)
					if b.Last {
						break
					}
				}
				if _, err := r.Read([]byte{}); err == io.EOF {
					break
				}
			}
		})
	}
}
