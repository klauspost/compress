// Copyright 2025+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

//go:build go1.24

package zstd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestEncodeTo(t *testing.T) {
	// TODO: When we can remove the build tag, integrate this into the main test suite.
	data, err := os.ReadFile("testdata/comp-crashers.zip")
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	for i, tt := range zr.File {
		if testing.Short() && i > 10 {
			break
		}

		t.Run(tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			in, err := io.ReadAll(r)
			if err != nil {
				t.Error(err)
			}
			encoded := EncodeTo(make([]byte, 0, len(in)/2), in)
			got, err := DecodeTo(make([]byte, 0, len(in)/2), encoded)
			if err != nil {
				t.Logf("error: %v\nwant: %v\ngot:  %v", err, len(in), len(got))
				t.Fatal(err)
			}
			if !bytes.Equal(in, got) {
				t.Errorf("decode mismatch for %s: want %d, got %d", tt.Name, len(in), len(got))
				t.Logf("want: %x\ngot:  %x", in, got)
			}
		})
	}
}
