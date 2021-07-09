package fse

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"
)

func TestWriter(t *testing.T) {
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
			enc, err := NewWriter(wrt, WriterWith.CRC(true))
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
		})
	}
}

func BenchmarkWriter(b *testing.B) {
	for _, tt := range testfiles {
		test := tt
		b.Run(test.name, func(b *testing.B) {
			buf0, err := test.fn()
			if err != nil {
				b.Fatal(err)
			}
			buf := bytes.NewBuffer(buf0)
			for buf.Len() < 1<<20 {
				buf.Write(buf0)
			}
			buf0 = buf.Bytes()
			w, err := NewWriter(ioutil.Discard, WriterWith.CRC(false))
			if err != nil {
				b.Fatal("unexpected error:", err)
			}
			_, _ = w.Write(buf0)
			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(buf.Len()))
			for i := 0; i < b.N; i++ {
				w.Reset(ioutil.Discard)
				buf = bytes.NewBuffer(buf0)
				_, err = io.Copy(w, buf)
				if err != nil {
					b.Fatal(err)
				}
				err = w.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
