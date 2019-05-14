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
	s := Snappy{}
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
		ioutil.WriteFile("testdata/z000028.got", decoded, os.ModePerm)
		t.Fatal("Decoded does not match")
	}
	t.Log("Encoded content matched")
}
