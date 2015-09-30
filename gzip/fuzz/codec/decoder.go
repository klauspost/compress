// +build gofuzz

package codec

import (
	"bytes"
	"io"
	"io/ioutil"

	gzip "github.com/klauspost/compress/gzip"
)

func Fuzz(data []byte) int {
	fr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	if len(fr.Comment) > 1<<20 || len(fr.Name) > 1<<20 || len(fr.Extra) > 1<<20 {
		panic("huge header")
	}
	uncomp := make([]byte, 64<<10)
	n, err := fr.Read(uncomp)
	if err != nil && err != io.EOF {
		return 0
	}
	if n == len(uncomp) {
		return 0 // too large
	}
	uncomp = uncomp[:n]
	for c := 0; c <= 9; c++ {
		buf := new(bytes.Buffer)
		gw, err := gzip.NewWriterLevel(buf, c)
		if err != nil {
			panic(err)
		}
		gw.Header = fr.Header
		n, err := gw.Write(uncomp)
		if err != nil {
			panic(err)
		}
		if n != len(uncomp) {
			panic("short write")
		}
		if err := gw.Close(); err != nil {
			panic(err)
		}
		fr1, err := gzip.NewReader(buf)
		if err != nil {
			panic(err)
		}
		uncomp1, err := ioutil.ReadAll(fr1)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(uncomp, uncomp1) {
			panic("data differs")
		}
	}
	return 1
}
