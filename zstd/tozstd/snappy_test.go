package tozstd

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zstd"
)

func TestSnappy_ConvertSimple(t *testing.T) {
	in, err := ioutil.ReadFile("testdata/z000028")
	if err != nil {
		t.Fatal(err)
	}

	var comp bytes.Buffer
	w := snappy.NewBufferedWriter(&comp)
	_, err = io.Copy(w, bytes.NewBuffer(in))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	snapLen := comp.Len()
	s := SnappyConverter{}
	var dst bytes.Buffer
	n, err := s.Convert(&comp, &dst)
	if err != io.EOF {
		t.Fatal(err)
	}
	if n != int64(dst.Len()) {
		t.Errorf("Dest was %d bytes, but said to have written %d bytes", dst.Len(), n)
	}
	t.Log("SnappyConverter len", snapLen, "-> zstd len", dst.Len())

	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dec.DecodeAll(dst.Bytes(), nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/z000028.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestSnappy_ConvertXML(t *testing.T) {
	in, err := ioutil.ReadFile("testdata/xml")
	if err != nil {
		t.Fatal(err)
	}

	var comp bytes.Buffer
	w := snappy.NewBufferedWriter(&comp)
	_, err = io.Copy(w, bytes.NewBuffer(in))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	snapLen := comp.Len()
	s := SnappyConverter{}
	var dst bytes.Buffer
	n, err := s.Convert(&comp, &dst)
	if err != io.EOF {
		t.Fatal(err)
	}
	if n != int64(dst.Len()) {
		t.Errorf("Dest was %d bytes, but said to have written %d bytes", dst.Len(), n)
	}
	t.Log("Snappy len", snapLen, "-> zstd len", dst.Len())

	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dec.DecodeAll(dst.Bytes(), nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/xml.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func TestSnappy_ConvertSilesia(t *testing.T) {
	in, err := ioutil.ReadFile("../testdata/silesia.tar")
	if err != nil {
		t.Fatal(err)
	}

	var comp bytes.Buffer
	w := snappy.NewBufferedWriter(&comp)
	_, err = io.Copy(w, bytes.NewBuffer(in))
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	snapLen := comp.Len()
	s := SnappyConverter{}
	var dst bytes.Buffer
	n, err := s.Convert(&comp, &dst)
	if err != io.EOF {
		t.Fatal(err)
	}
	if n != int64(dst.Len()) {
		t.Errorf("Dest was %d bytes, but said to have written %d bytes", dst.Len(), n)
	}
	t.Log("SnappyConverter len", snapLen, "-> zstd len", dst.Len())

	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := dec.DecodeAll(dst.Bytes(), nil)
	if err != nil {
		t.Error(err, len(decoded))
	}
	if !bytes.Equal(decoded, in) {
		ioutil.WriteFile("testdata/silesia.tar.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}

func BenchmarkSnappy_ConvertXML(b *testing.B) {
	in, err := ioutil.ReadFile("testdata/xml")
	if err != nil {
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

func BenchmarkSnappy_ConvertSilesia(b *testing.B) {
	in, err := ioutil.ReadFile("../testdata/silesia.tar")
	if err != nil {
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
