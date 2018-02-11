package fse

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
)

func TestNewReader(t *testing.T) {
	for _, test := range testfiles {
		t.Run(test.name, func(t *testing.T) {
			buf0, err := test.fn()
			if err != nil {
				t.Fatal(err)
			}
			buf := bytes.NewBuffer(buf0)
			for buf.Len() < 1<<20 {
				buf.Write(buf0)
			}
			buf0 = buf.Bytes()
			wrt := &bytes.Buffer{}
			enc, err := NewWriter(wrt, WithWriterOption.CRC(true))
			if err != nil {
				t.Fatal(err)
			}
			n, err := io.Copy(enc, buf)
			if err != nil {
				t.Fatal(err)
			}
			if n != int64(len(buf0)) {
				t.Fatalf("want to write %d, wrote %d", len(buf0), n)
			}
			err = enc.Close()
			if err != nil {
				t.Fatal(err)
			}
			b := wrt.Bytes()
			t.Logf("%s: %d -> %d bytes (%.2f:1)", test.name, len(buf0), len(b), float64(len(buf0))/float64(len(b)))

			dec, err := NewReader(bytes.NewBuffer(b))
			if err != nil {
				t.Fatal(err)
			}
			var gotBack = &bytes.Buffer{}
			n, err = io.Copy(gotBack, dec)
			if err != nil {
				t.Fatal(err)
			}
			if int(n) != len(buf0) {
				t.Fatalf("want %d bytes, got %d", len(buf0), n)
			}
			if int(n) != gotBack.Len() {
				t.Fatalf("byte count mismatch, n: %d, buffer: %d", n, gotBack.Len())
			}
			if !bytes.Equal(gotBack.Bytes(), buf0) {
				t.Error("bytes mismatch")
			}
			dec.Reset(bytes.NewBuffer(b))
			if err != nil {
				t.Fatal(err)
			}
			gotBack = &bytes.Buffer{}
			n, err = io.Copy(gotBack, dec)
			if err != nil {
				t.Fatal(err)
			}
			if int(n) != len(buf0) {
				t.Fatalf("want %d bytes, got %d", len(buf0), n)
			}
			if int(n) != gotBack.Len() {
				t.Fatalf("byte count mismatch, n: %d, buffer: %d", n, gotBack.Len())
			}
			if !bytes.Equal(gotBack.Bytes(), buf0) {
				t.Error("bytes mismatch")
			}
		})
	}
}

func BenchmarkReader(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		b.Run(test.name, func(b *testing.B) {
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			buf := bytes.NewBuffer(nil)
			for buf.Len() < 10<<20 {
				buf.Write(buf0)
			}
			buf0 = buf.Bytes()
			compressed := bytes.NewBuffer(nil)
			w, err := NewWriter(compressed, WithWriterOption.CRC(false))
			if err != nil {
				b.Fatal("unexpected error:", err)
			}
			_, err = io.Copy(w, buf)
			if err != nil {
				b.Fatal(err)
			}
			w.Close()
			r, err := NewReader(bytes.NewBuffer(compressed.Bytes()))
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(buf0)))
			for i := 0; i < b.N; i++ {
				err := r.Reset(bytes.NewBuffer(compressed.Bytes()))
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.Copy(ioutil.Discard, r)
				if err != nil {
					b.Fatal(err)
				}
				if n != int64(len(buf0)) {
					b.Fatal(len(buf0), "!=", n)
				}
			}
		})
	}
}
