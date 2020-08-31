package zstd

import (
	"bytes"
	"fmt"
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
	var dicts [][]byte
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
			dicts = append(dicts, in)
		}()
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderDicts(dicts...))
	if err != nil {
		t.Fatal(err)
		return
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

func TestEncoder_SmallDict(t *testing.T) {
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
	var dicts [][]byte
	var encs []*Encoder
	var noDictEncs []*Encoder
	var encNames []string

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
			dicts = append(dicts, in)
			for level := SpeedFastest; level < speedLast; level++ {
				enc, err := NewWriter(nil, WithEncoderConcurrency(1), WithEncoderDict(in), WithEncoderLevel(level), WithWindowSize(1<<17))
				if err != nil {
					t.Fatal(err)
				}
				defer enc.Close()
				encs = append(encs, enc)
				encNames = append(encNames, fmt.Sprint("level-", level.String(), "-dict-", len(dicts)))

				enc, err = NewWriter(nil, WithEncoderConcurrency(1), WithEncoderLevel(level), WithWindowSize(1<<17))
				if err != nil {
					t.Fatal(err)
				}
				noDictEncs = append(noDictEncs, enc)
				defer enc.Close()
			}
		}()
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderDicts(dicts...))
	if err != nil {
		t.Fatal(err)
		return
	}
	defer dec.Close()
	for i, tt := range zr.File {
		if testing.Short() && i > 100 {
			break
		}
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		in, err := ioutil.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := dec.DecodeAll(in, nil)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("encodeall-"+tt.Name, func(t *testing.T) {
			// Attempt to compress with all dicts
			var b []byte
			var tmp []byte
			for i := range encs {
				t.Run(encNames[i], func(t *testing.T) {
					b = encs[i].EncodeAll(decoded, b[:0])
					tmp, err = dec.DecodeAll(in, tmp[:0])
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(tmp, decoded) {
						t.Fatal("output mismatch")
					}

					tmp = noDictEncs[i].EncodeAll(decoded, tmp[:0])

					if strings.Contains(t.Name(), "dictplain") && strings.Contains(t.Name(), "dict-6") {
						t.Log("reference:", len(in), "no dict:", len(tmp), "with dict:", len(b), "SAVED:", len(tmp)-len(b))
						// Check that we reduced this significantly
						if len(b) > 1000 {
							t.Error("output was bigger than expected")
						}
					}
				})
			}
		})
		t.Run("stream-"+tt.Name, func(t *testing.T) {
			// Attempt to compress with all dicts
			var tmp []byte
			for i := range encs {
				enc := encs[i]
				t.Run(encNames[i], func(t *testing.T) {
					var buf bytes.Buffer
					enc.Reset(&buf)
					_, err := enc.Write(decoded)
					if err != nil {
						t.Fatal(err)
					}
					err = enc.Close()
					if err != nil {
						t.Fatal(err)
					}
					tmp, err = dec.DecodeAll(buf.Bytes(), tmp[:0])
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(tmp, decoded) {
						t.Fatal("output mismatch")
					}

					tmp = noDictEncs[i].EncodeAll(decoded, tmp[:0])

					if strings.Contains(t.Name(), "dictplain") && strings.Contains(t.Name(), "dict-6") {
						t.Log("reference:", len(in), "no dict:", len(tmp), "with dict:", buf.Len(), "SAVED:", len(tmp)-buf.Len())
						// Check that we reduced this significantly
						if buf.Len() > 1000 {
							t.Error("output was bigger than expected")
						}
					}
				})
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

	var dicts [][]byte
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
			dicts = append(dicts, in)
		}()
	}
	dec, err := NewReader(nil, WithDecoderConcurrency(1), WithDecoderDicts(dicts...))
	if err != nil {
		t.Fatal(err)
		return
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
