package flate

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/klauspost/compress/internal/fuzz"
)

// Fuzzing tweaks:
var fuzzStartF = flag.Int("start", HuffmanOnly, "Start fuzzing at this level")
var fuzzEndF = flag.Int("end", UltraCompression, "End fuzzing at this level (inclusive)")
var fuzzMaxF = flag.Int("max", 1<<20, "Maximum input size")
var fuzzSLF = flag.Bool("sl", true, "Include stateless encodes")
var fuzzWindow = flag.Bool("windows", true, "Include windowed encodes")
var fuzzDict = flag.Bool("dict", false, "Only run dictionary encodes")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

// FuzzEncodingFlush writes the input in chunks with a Flush after each, so
// encoder state is carried across sync points, at every level.
// The first byte selects the encoder, the second the chunk sizes, a dictionary
// and extra empty flushes.
func FuzzEncodingFlush(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/regression.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/fuzz/encode-raw-corpus.zip", fuzz.TypeRaw, testing.Short())

	decoder := NewReader(nil)
	buf := new(bytes.Buffer)
	var encs []*Writer
	var names []string
	for level := HuffmanOnly; level <= UltraCompression; level++ {
		w, err := NewWriter(nil, level)
		if err != nil {
			f.Fatal(err)
		}
		encs = append(encs, w)
		names = append(names, "level "+strconv.Itoa(level))
	}
	w, err := NewWriterWindow(nil, 1000)
	if err != nil {
		f.Fatal(err)
	}
	encs = append(encs, w)
	names = append(names, "window 1000")
	// Long matches across flushes, at every encoder.
	text := bytes.Repeat([]byte("Long matches cross the flush points. "), 1500)
	for i := range encs {
		for _, mode := range []byte{0, 6, 16 | 2, 32 | 9} {
			f.Add(append([]byte{byte(i), mode}, text...))
		}
	}

	// An input picks one encoder and flushes up to maxFlushes times per stream,
	// which keeps it well below the 10s after which a fuzzing worker counts as hung.
	const maxSize, maxUltra, maxFlushes = 128 << 10, 64 << 10, 256
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 2 || len(data) > min(*fuzzMaxF, maxSize) {
			return
		}
		i := int(data[0]) % len(encs)
		fw := encs[i]
		mode := data[1]
		data = data[2:]
		if fw.d.level == UltraCompression && len(data) > maxUltra {
			return
		}
		// Chunks are a byte of the input shifted by 0-15 bits, plus one,
		// so they range from single bytes to the whole input.
		shift := mode & 15
		var dict []byte
		if mode&16 != 0 {
			dict = data[:len(data)/2]
		}
		// The second round tests that Reset restores the state and dictionary.
		for _, reset := range []bool{false, true} {
			msg := names[i] + " reset " + strconv.FormatBool(reset) + ":"
			buf.Reset()
			if reset {
				fw.Reset(buf)
			} else {
				fw.ResetDict(buf, dict)
			}
			for j, flushes := 0, 0; j < len(data); flushes++ {
				n := min(int(data[j])<<shift+1, len(data)-j)
				if flushes == maxFlushes {
					n = len(data) - j
				}
				if _, err := fw.Write(data[j : j+n]); err != nil {
					t.Fatal(msg + err.Error())
				}
				j += n
				for range 1 + int(mode>>5&1) {
					if err := fw.Flush(); err != nil {
						t.Fatal(msg + err.Error())
					}
				}
			}
			if err := fw.Close(); err != nil {
				t.Fatal(msg + err.Error())
			}
			decoder.(Resetter).Reset(buf, dict)
			got, err := io.ReadAll(decoder)
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			if !bytes.Equal(data, got) {
				t.Fatal(msg + "not equal")
			}
		}
	})
}

func FuzzEncoding(f *testing.F) {
	fuzz.AddFromZip(f, "testdata/regression.zip", fuzz.TypeRaw, false)
	fuzz.AddFromZip(f, "testdata/fuzz/encode-raw-corpus.zip", fuzz.TypeRaw, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/FuzzEncoding.zip", fuzz.TypeGoFuzz, testing.Short())
	fuzz.AddFromZip(f, "testdata/fuzz/encode-oss-corpus.zip", fuzz.TypeOSSFuzz, testing.Short())

	startFuzz := *fuzzStartF
	endFuzz := *fuzzEndF
	maxSize := *fuzzMaxF
	stateless := *fuzzSLF
	fuzzWindow := *fuzzWindow
	fuzzDict := *fuzzDict
	if fuzzDict {
		// Dictionary encodes are 4 roundtrips per level, so keep inputs small
		// enough that the fuzzing worker isn't considered hung.
		maxSize = min(maxSize, 256<<10)
	}
	// Level 10 is much slower than the others.
	const maxUltra = 128 << 10

	decoder := NewReader(nil)
	buf := new(bytes.Buffer)
	encs := make([]*Writer, endFuzz-startFuzz+1)
	for i := range encs {
		var err error
		encs[i], err = NewWriter(nil, i+startFuzz)
		if err != nil {
			f.Fatal(err.Error())
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSize {
			return
		}
		if fuzzDict {
			// One dictionary that is unlikely to match the input and one that matches.
			for di, dict := range [][]byte{[]byte("0123456789abcdefghij"), data[len(data)/2:]} {
				for level := startFuzz; level <= endFuzz; level++ {
					if level == UltraCompression && len(data) > maxUltra {
						continue
					}
					fw := encs[level-startFuzz]
					// Second round tests that Reset re-applies the dictionary.
					for _, reset := range []bool{false, true} {
						msg := "dict " + strconv.Itoa(di) + " level " + strconv.Itoa(level) + " reset " + strconv.FormatBool(reset) + ":"
						buf.Reset()
						if reset {
							fw.Reset(buf)
						} else {
							fw.ResetDict(buf, dict)
						}
						n, err := fw.Write(data)
						if n != len(data) {
							t.Fatal(msg + "short write")
						}
						if err != nil {
							t.Fatal(msg + err.Error())
						}
						if err := fw.Close(); err != nil {
							t.Fatal(msg + err.Error())
						}
						decoder.(Resetter).Reset(buf, dict)
						data2, err := io.ReadAll(decoder)
						if err != nil {
							t.Fatal(msg + err.Error())
						}
						if !bytes.Equal(data, data2) {
							t.Fatal(msg + "not equal")
						}
					}
				}
			}
			return
		}
		for level := startFuzz; level <= endFuzz; level++ {
			if level == UltraCompression && len(data) > maxUltra {
				continue
			}
			msg := "level " + strconv.Itoa(level) + ":"
			buf.Reset()
			fw := encs[level-startFuzz]
			fw.Reset(buf)
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
			// Do it again... (also uses copy)
			msg = "level " + strconv.Itoa(level) + " (reset):"
			buf.Reset()
			fw.Reset(buf)
			_, err = io.Copy(fw, bytes.NewReader(data))
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
		if stateless {
			// Split into two and use history...
			msg := "stateless:"
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
				t.Error(msg + "not equal")
			}
		}
		if fuzzWindow {
			msg := "windowed:"
			buf.Reset()
			fw, err := NewWriterWindow(buf, 1000)
			if err != nil {
				t.Fatal(msg + err.Error())
			}
			fw.Reset(buf)
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
			msg = msg + " (reset):"
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
		}
	})
}
