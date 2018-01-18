package fse

import (
	"io/ioutil"
	"testing"
)

const (
	digits = iota
	twain
	random
)

var testfiles = [][2]string{
	// Digits is the digits of the irrational number e. Its decimal representation
	// does not repeat, but there are only 10 possible digits, so it should be
	// reasonably compressible.
	digits: {"numbers", "../testdata/e.txt"},
	// Twain is Project Gutenberg's edition of Mark Twain's classic English novel.
	twain: {"twain", "../testdata/Mark.Twain-Tom.Sawyer.txt"},
	// Random bytes
	random: {"random", "../testdata/sharnd.out"},
}

func TestCompress(t *testing.T) {
	for i := range testfiles {
		var s Scratch
		t.Run(testfiles[i][0], func(t *testing.T) {
			buf0, err := ioutil.ReadFile(testfiles[i][1])
			if err != nil {
				t.Fatal(err)
			}
			b, err := Compress(buf0, &s)
			if err != nil {
				t.Error(err)
			}
			if b == nil {
				t.Log("not compressible")
				return
			}
			t.Logf("%d -> %d bytes", len(buf0), len(b))
			t.Logf("%v", b)
		})
	}

}
