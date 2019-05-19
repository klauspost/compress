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
