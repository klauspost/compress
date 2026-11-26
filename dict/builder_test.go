package dict

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestZStdDict(t *testing.T) {
	for _, level := range []zstd.EncoderLevel{zstd.SpeedFastest, zstd.SpeedDefault, zstd.SpeedBetterCompression, zstd.SpeedBestCompression, zstd.SpeedUltraCompression} {
		testZStdDict(t, level)
	}
}

func testZStdDict(t *testing.T, level zstd.EncoderLevel) {
	out := io.Discard
	if testing.Verbose() {
		out = os.Stdout
	}
	opts := Options{
		MaxDictSize:    2048,
		HashBytes:      4,
		Output:         out,
		ZstdDictID:     0,
		ZstdDictCompat: false,
		ZstdLevel:      level,
	}

	inBuf := make([]byte, 0, 4096)
	outBuf := make([]byte, 0, 4096)

	// This is 32K worth of data, but it's all very similar. Only fits in 4K if compressed with a dictionary.
	samples := generateSimilarByteSlices(42, 32)

	dict, err := BuildZstdDict(samples, opts)
	if err != nil {
		t.Fatal(err.Error())
	}

	totalSize := 0
	for _, blob := range samples {
		compressed, err := zCompressDict(inBuf, dict, blob)
		if err != nil {
			t.Fatal(err.Error())
		}
		totalSize += len(compressed)

		// Check round trip.
		decompressed, err := zDecompressDict(outBuf, dict, compressed)
		if err != nil {
			t.Fatal(err.Error())
		}
		if !bytes.Equal(decompressed, blob) {
			t.Fatal("Round trip failed")
		}
	}
	if totalSize > 4096 {
		t.Fatal("Total compressed size exceeds 4096 bytes")
	}
	t.Log("Total compressed size:", totalSize)
}

func zCompressDict(dst, dict, data []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderDict(dict))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()

	result := encoder.EncodeAll(data, dst[:0])
	return result, nil
}

func zDecompressDict(dst, dict, data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderDicts(dict))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	result, err := decoder.DecodeAll(data, dst[:0])
	if err != nil {
		return nil, err
	}

	return result, nil
}

// Creates a slice of byte slices, each of which is has the same random seed, so they are very similar. The length
// of each slice is 1024 + the index of the slice.
func generateSimilarByteSlices(seed int64, count int) [][]byte {
	chks := make([][]byte, count)
	for i := range count {
		chks[i] = generateRandomByteSlice(seed, 1024+i)
		if false {
			// Generate a small diff.
			chks[i][i] = byte(seed)
		}
	}

	return chks
}

func generateRandomByteSlice(seed int64, len int) []byte {
	r := rand.NewSource(seed)

	data := make([]byte, len)
	for i := range data {
		data[i] = byte(r.Int63())
	}
	return data
}

var builders = []struct {
	name  string
	build func([][]byte, Options) ([]byte, error)
}{
	{"raw", BuildRawDict},
	{"s2", BuildS2Dict},
	{"zstd", BuildZstdDict},
}

func TestBuildDictInvalidInput(t *testing.T) {
	sample := []byte("the quick brown fox jumps over the lazy dog")
	rep := bytes.Repeat([]byte("abcdefgh"), 64)
	shared := [][]byte{rep, rep, append([]byte("zz"), rep...)}
	for _, tt := range []struct {
		name  string
		input [][]byte
		size  int
	}{
		{"short", [][]byte{[]byte("short"), []byte("tiny")}, 4096},
		{"single", [][]byte{sample}, 4096},
		{"identical", [][]byte{sample, sample}, 4096},
		{"disjoint", [][]byte{[]byte("0123456789abcdef"), []byte("ghijklmnopqrstuv")}, 4096},
		{"size0", shared, 0},
		{"size-1", shared, -1},
	} {
		for _, b := range builders {
			t.Run(tt.name+"-"+b.name, func(t *testing.T) {
				if _, err := b.build(tt.input, Options{HashBytes: 6, MaxDictSize: tt.size}); err == nil {
					t.Fatal("want error")
				}
			})
		}
	}
}

func jsonRecords(rng *rand.Rand, n int) [][]byte {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	records := make([][]byte, n)
	for i := range records {
		records[i] = fmt.Appendf(nil, `{"id":%d,"name":%q,"tags":[%q,%q],"score":%d}`, rng.Intn(100000),
			words[rng.Intn(len(words))], words[rng.Intn(len(words))], words[rng.Intn(len(words))], rng.Intn(1000))
	}
	return records
}

// Building twice from the same input gives the same dictionary,
// whatever the order the builder's maps are iterated in.
func TestBuildDictDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	records := jsonRecords(rng, 100)
	// Different values at the same place in equal shares of the samples
	// tie as followers on both count and offset.
	tied := make([][]byte, 100)
	for i := range tied {
		tied[i] = fmt.Appendf(nil, "commonprefix_%s_suffixcommon_%08x", strings.Repeat(string(rune('A'+i%4)), 16), rng.Uint32())
	}
	for _, set := range []struct {
		name    string
		samples [][]byte
	}{{"records", records}, {"tied", tied}} {
		for _, b := range builders {
			for _, hb := range []int{4, 6, 8} {
				t.Run(fmt.Sprintf("%s-%s-%d", set.name, b.name, hb), func(t *testing.T) {
					o := Options{MaxDictSize: 4096, HashBytes: hb}
					want, err := b.build(set.samples, o)
					if err != nil {
						t.Fatal(err)
					}
					for range 2 {
						got, err := b.build(set.samples, o)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(got, want) {
							t.Fatal("dictionary differs between builds")
						}
					}
				})
			}
		}
	}
}
