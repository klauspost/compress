package zstd

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"
)

func TestEncoder_EncodeAllSimple(t *testing.T) {
	in, err := ioutil.ReadFile("testdata/z000028")
	if err != nil {
		t.Fatal(err)
	}
	in = append(in, in...)
	var e Encoder
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))

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
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))

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
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))

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

	var e Encoder
	dst := e.EncodeAll(in, nil)
	t.Log("Simple Encoder len", len(in), "-> zstd len", len(dst))
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
