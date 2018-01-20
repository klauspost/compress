package fse

import (
	"io/ioutil"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const (
	digits = iota
	twain
	random
)

type inputFn func() ([]byte, error)

var testfiles = []struct {
	name string
	fn   inputFn
}{
	// Digits is the digits of the irrational number e. Its decimal representation
	// does not repeat, but there are only 10 possible digits, so it should be
	// reasonably compressible.
	digits: {name: "numbers", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/e.txt") }},
	// Twain is Project Gutenberg's edition of Mark Twain's classic English novel.
	twain: {name: "twain", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/Mark.Twain-Tom.Sawyer.txt") }},
	// Random bytes
	random: {name: "random", fn: func() ([]byte, error) { return ioutil.ReadFile("../testdata/sharnd.out") }},
}

func TestCompress(t *testing.T) {
	for i := range testfiles {
		var s Scratch
		t.Run(testfiles[i].name, func(t *testing.T) {
			buf0, err := testfiles[i].fn()
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

func TestReadNCount(t *testing.T) {
	for i := range testfiles {
		var s Scratch
		name := testfiles[i].name
		t.Run(name, func(t *testing.T) {
			name += ": "
			buf0, err := testfiles[i].fn()
			if err != nil {
				t.Fatal(err)
			}
			b, err := Compress(buf0, &s)
			if err != nil {
				t.Error(err)
				return
			}
			if b == nil {
				t.Log(name + "not compressible")
				return
			}
			t.Logf(name+"%d -> %d bytes", len(buf0), len(b))
			//t.Logf("%v", b)
			var s2 Scratch
			_, err = Decompress(b, &s2)
			if err != nil {
				t.Fatal(err)
			}
			want := s.norm[:s.symbolLen]
			got := s2.norm[:s2.symbolLen]
			if !cmp.Equal(want, got) {
				if s.actualTableLog != s2.actualTableLog {
					t.Errorf(name+"norm table, want tablelog: %d, got %d", s.actualTableLog, s2.actualTableLog)
				}
				if s.symbolLen != s2.symbolLen {
					t.Errorf(name+"norm table, want size: %d, got %d", s.symbolLen, s2.symbolLen)
				}
				t.Errorf(name+"norm table, got delta: \n%s", cmp.Diff(want, got))
				return
			}
		})
	}
}
