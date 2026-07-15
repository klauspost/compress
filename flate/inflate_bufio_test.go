package flate

import (
	"bufio"
	"bytes"
	"io"
	"testing"
	"testing/iotest"
)

// roundtripBufio round-trips data through readers that force the *bufio.Reader
// decode path (huffmanBufioReader, which reads via Peek and Discards consumed
// bytes at each return), asserting byte-for-byte equality. The OneByte/Half
// wrappers stress the Peek-refill boundaries.
func roundtripBufio(t *testing.T, data []byte) {
	t.Helper()
	for _, lvl := range []int{0, 1, 6, 9} {
		var comp bytes.Buffer
		w, err := NewWriter(&comp, lvl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		c := comp.Bytes()
		for name, mk := range map[string]func() io.Reader{
			"onebyte": func() io.Reader { return iotest.OneByteReader(bytes.NewReader(c)) },
			"half":    func() io.Reader { return iotest.HalfReader(bytes.NewReader(c)) },
			"bufio":   func() io.Reader { return bufio.NewReader(bytes.NewReader(c)) },
		} {
			got, err := io.ReadAll(NewReader(mk()))
			if err != nil {
				t.Fatalf("level %d, %s reader: %v", lvl, name, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("level %d, %s reader: round-trip mismatch (%d vs %d bytes)", lvl, name, len(got), len(data))
			}
		}
	}
}

func FuzzInflateBufio(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("a"))
	f.Add(bytes.Repeat([]byte("hello world "), 5000))
	f.Add(bytes.Repeat([]byte{0}, 100000))
	seq := make([]byte, 20000)
	for i := range seq {
		seq[i] = byte(i*31 + i/7)
	}
	f.Add(seq)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<18 { // keep fuzz execs fast; larger inputs add no new paths here
			return
		}
		roundtripBufio(t, data)
	})
}
