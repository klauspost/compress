//go:build go1.18
// +build go1.18

package flate

import (
	"bytes"
	"io"
	"strconv"
	"testing"

	"github.com/klauspost/compress/internal/fuzz"
)

func FuzzEncoding(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/regression.zip", true, false)
	fuzz.AddFromZip(f, "testdata/fuzz/encode-raw-corpus.zip", true, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/FuzzEncoding.zip", false, testing.Short())
	// Fuzzing tweaks:
	const (
		// Test a subset of encoders.
		startFuzz = HuffmanOnly
		endFuzz   = BestCompression

		// Also tests with dictionaries...
		testDicts = true

		// Max input size:
		maxSize = 1 << 20
	)
	decoder := NewReader(nil)
	buf := new(bytes.Buffer)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSize {
			return
		}
		for level := startFuzz; level <= endFuzz; level++ {
			msg := "level " + strconv.Itoa(level) + ":"
			buf.Reset()
			fw, err := NewWriter(buf, level)
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			n, err := fw.Write(data)
			if n != len(data) {
				t.Fatal(msg + "short write")
			}
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			err = fw.Close()
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			decoder.(Resetter).Reset(buf, nil)
			data2, err := io.ReadAll(decoder)
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			if !bytes.Equal(data, data2) {
				t.Fatal(msg + "not equal")
			}
			// Do it again...
			msg = "level " + strconv.Itoa(level) + " (reset):"
			buf.Reset()
			fw.Reset(buf)
			n, err = fw.Write(data)
			if n != len(data) {
				t.Fatal(msg + "short write")
			}
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			err = fw.Close()
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			decoder.(Resetter).Reset(buf, nil)
			data2, err = io.ReadAll(decoder)
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			if !bytes.Equal(data, data2) {
				t.Fatal(msg + "not equal")
			}
		}

		// Split into two and use history...
		buf.Reset()
		err := StatelessDeflate(buf, data[:len(data)/2], false, nil)
		if err != nil {
			t.Error(err)
		}

		// Use top half as dictionary...
		dict := data[:len(data)/2]
		err = StatelessDeflate(buf, data[len(data)/2:], true, dict)
		if err != nil {
			t.Error(err)
		}

		decoder.(Resetter).Reset(buf, nil)
		data2, err := io.ReadAll(decoder)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(data, data2) {
			//fmt.Printf("want:%x\ngot: %x\n", data1, data2)
			t.Error("not equal")
		}
	})
}
