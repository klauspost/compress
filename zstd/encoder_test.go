// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
)

func TestEncoder_EncodeAllSimple(t *testing.T) {
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
		ioutil.WriteFile("testdata/"+t.Name()+"-z000028.got", decoded, os.ModePerm)
		ioutil.WriteFile("testdata/"+t.Name()+"-z000028.want", in, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoder_EncodeXML(t *testing.T) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	in, err := ioutil.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}

	var e Encoder
	start := time.Now()
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/"+t.Name()+"-xml.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoderRegression(t *testing.T) {
	defer timeout(30 * time.Second)()
	data, err := ioutil.ReadFile("testdata/comp-crashers.zip")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := NewWriter(nil, WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewReader(nil)
	if err != nil {
		t.Error(err)
		return
	}
	// We can't close the decoder.

	for _, tt := range zr.File {
		if !strings.HasSuffix(t.Name(), "") {
			continue
		}

		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Error(err)
			}
			encoded := enc.EncodeAll(in, nil)
			got, err := dec.DecodeAll(encoded, nil)
			if err != nil {
				//ref, refErr := zstd.Decompress(nil, encoded)
				//t.Logf("error: %v (ref:%v)\nwant: %v\nref:  %v\ngot:  %v", err, refErr, in, ref, got)
				t.Logf("error: %v\nwant: %v\ngot:  %v", err, in, got)
				t.Fatal(err)
			}
		})
	}
}

func TestEncoder_EncodeAllTwain(t *testing.T) {
	in, err := ioutil.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt")
	if err != nil {
		t.Fatal(err)
	}
	//in = append(in, in...)
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
		ioutil.WriteFile("testdata/"+t.Name()+"-Mark.Twain-Tom.Sawyer.txt.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoder_EncodeAllPi(t *testing.T) {
	in, err := ioutil.ReadFile("../testdata/pi.txt")
	if err != nil {
		t.Fatal(err)
	}
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
		ioutil.WriteFile("testdata/"+t.Name()+"-pi.txt.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoder_EncodeAllSilesia(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	in, err := ioutil.ReadFile("testdata/silesia.tar")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("Missing testdata/silesia.tar")
			return
		}
		t.Fatal(err)
	}

	var e Encoder
	start := time.Now()
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)

	dec, err := NewReader(nil, WithDecoderMaxMemory(220<<20))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/"+t.Name()+"-silesia.tar.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestEncoder_EncodeAllEnwik9(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/enwik9.zst"
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("To run extended tests, download http://mattmahoney.net/dc/enwik9.zip unzip it \n" +
				"compress it with 'zstd -15 -T0 enwik9' and place it in " + file)
		}
	}
	dec, err := NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	in, err := ioutil.ReadAll(dec)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	var e Encoder
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
	mbpersec := (float64(len(in)) / (1024 * 1024)) / (float64(time.Since(start)) / (float64(time.Second)))
	t.Logf("Encoded %d bytes with %.2f MB/s", len(in), mbpersec)
	decoded, err := dec.DecodeAll(dst, nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/"+t.Name()+"-enwik9.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func BenchmarkEncoder_EncodeAllXML(b *testing.B) {
	f, err := os.Open("testdata/xml.zst")
	if err != nil {
		b.Fatal(err)
	}
	dec, err := NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(dec)
	if err != nil {
		b.Fatal(err)
	}
	dec.Close()

	enc := Encoder{}
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.Log("Output size:", len(dst))
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllSimple(b *testing.B) {
	f, err := os.Open("testdata/z000028")
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(f)
	if err != nil {
		b.Fatal(err)
	}

	enc := Encoder{}
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllHTML(b *testing.B) {
	f, err := os.Open("../testdata/html.txt")
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(f)
	if err != nil {
		b.Fatal(err)
	}

	enc := Encoder{}
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllTwain(b *testing.B) {
	f, err := os.Open("../testdata/Mark.Twain-Tom.Sawyer.txt")
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(f)
	if err != nil {
		b.Fatal(err)
	}

	enc := Encoder{}
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

func BenchmarkEncoder_EncodeAllPi(b *testing.B) {
	f, err := os.Open("../testdata/pi.txt")
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(f)
	if err != nil {
		b.Fatal(err)
	}

	enc := Encoder{}
	dst := enc.EncodeAll(in, nil)
	wantSize := len(dst)
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		dst := enc.EncodeAll(in, dst[:0])
		if len(dst) != wantSize {
			b.Fatal(len(dst), "!=", wantSize)
		}
	}
}

/*
func BenchmarkSnappy_Enwik9(b *testing.B) {
	f, err := os.Open("testdata/enwik9.zst")
	if err != nil {
		b.Fatal(err)
	}
	dec, err := NewReader(f)
	if err != nil {
		b.Fatal(err)
	}
	in, err := ioutil.ReadAll(dec)
	if err != nil {
		b.Fatal(err)
	}
	defer dec.Close()

	var comp bytes.Buffer
	w := snappy.NewBufferedWriter(&comp)
	_, err = io.Copy(w, bytes.NewBuffer(in))
	if err != nil {
		b.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		b.Fatal(err)
	}
	s := SnappyConverter{}
	compBytes := comp.Bytes()
	_, err = s.Convert(&comp, ioutil.Discard)
	if err != io.EOF {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		_, err := s.Convert(bytes.NewBuffer(compBytes), ioutil.Discard)
		if err != io.EOF {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnappy_ConvertSilesia(b *testing.B) {
	in, err := ioutil.ReadFile("testdata/silesia.tar")
	if err != nil {
		if os.IsNotExist(err) {
			b.Skip("Missing testdata/silesia.tar")
			return
		}
		b.Fatal(err)
	}

	var comp bytes.Buffer
	w := snappy.NewBufferedWriter(&comp)
	_, err = io.Copy(w, bytes.NewBuffer(in))
	if err != nil {
		b.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		b.Fatal(err)
	}
	s := SnappyConverter{}
	compBytes := comp.Bytes()
	_, err = s.Convert(&comp, ioutil.Discard)
	if err != io.EOF {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		_, err := s.Convert(bytes.NewBuffer(compBytes), ioutil.Discard)
		if err != io.EOF {
			b.Fatal(err)
		}
	}
}
*/
