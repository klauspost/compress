package zstd

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
)

func TestNewDecoder(t *testing.T) {
	testDecoderFile(t, "testdata/decoder.zip")
	dec, err := NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, "testdata/decoder.zip", dec)
}

func TestNewDecoderGood(t *testing.T) {
	testDecoderFile(t, "testdata/good.zip")
	dec, err := NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, "testdata/good.zip", dec)
}

func TestNewDecoderBig(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/zstd-10kfiles.zip"
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Skip("To run extended tests, download https://files.klauspost.com/compress/zstd-10kfiles.zip \n" +
			"and place it in " + file + "\n" + "Running it requires about 5GB of RAM")
	}
	testDecoderFile(t, file)
	dec, err := NewDecoder(nil)
	if err != nil {
		t.Fatal(err)
	}
	testDecoderDecodeAll(t, file, dec)
}

func TestNewDecoderBigFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	file := "testdata/klauspost-paranoid-passwords.zst"
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Skip("To run extended tests, download https://files.klauspost.com/compress/klauspost-paranoid-passwords.zst \n" +
			"and place it in " + file)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start := time.Now()
	dec, err := NewDecoder(f)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(ioutil.Discard, dec)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	mbpersec := (float64(n) / (1024 * 1024)) / (float64(elapsed) / (float64(time.Second)))
	t.Logf("Decoded %d bytes with %f.2 MB/s", n, mbpersec)
}

func testDecoderFile(t *testing.T, fn string) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var want = make(map[string][]byte)
	for _, tt := range zr.File {
		if strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
			return
		}
		want[tt.Name+".zst"], _ = ioutil.ReadAll(r)
	}

	dec, err := NewDecoder(nil)
	if err != nil {
		t.Error(err)
		return
	}
	defer dec.Close()
	for _, tt := range zr.File {
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("Reader:"+tt.Name, func(t *testing.T) {
			r, err := tt.Open()
			if err != nil {
				t.Error(err)
				return
			}
			err = dec.Reset(r)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := ioutil.ReadAll(dec)
			if err != nil {
				if err == errNotimplemented {
					t.Skip(err)
					return
				}
				t.Error(err)
				if err != ErrCRCMismatch {
					return
				}
			}
			wantB := want[tt.Name]
			if !bytes.Equal(wantB, got) {
				if len(wantB)+len(got) < 1000 {
					t.Logf(" got: %v\nwant: %v", got, wantB)
				} else {
					fileName, _ := filepath.Abs(filepath.Join("testdata", t.Name()+"-want.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err := ioutil.WriteFile(fileName, wantB, os.ModePerm)
					t.Log("Wrote file", fileName, err)

					fileName, _ = filepath.Abs(filepath.Join("testdata", t.Name()+"-got.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err = ioutil.WriteFile(fileName, got, os.ModePerm)
					t.Log("Wrote file", fileName, err)
				}
				t.Logf("Length, want: %d, got: %d", len(wantB), len(got))
				t.Error("Output mismatch")
				return
			}
			t.Log(len(got), "bytes returned, matches input, ok!")
		})
	}
}

func testDecoderDecodeAll(t *testing.T, fn string, dec *Decoder) {
	data, err := ioutil.ReadFile(fn)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var want = make(map[string][]byte)
	for _, tt := range zr.File {
		if strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		r, err := tt.Open()
		if err != nil {
			t.Fatal(err)
			return
		}
		want[tt.Name+".zst"], _ = ioutil.ReadAll(r)
	}

	for _, tt := range zr.File {
		tt := tt
		if !strings.HasSuffix(tt.Name, ".zst") {
			continue
		}
		t.Run("ReadAll:"+tt.Name, func(t *testing.T) {
			t.Parallel()
			r, err := tt.Open()
			if err != nil {
				t.Fatal(err)
			}
			in, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			got, err := dec.DecodeAll(in, nil)
			wantB := want[tt.Name]
			if !bytes.Equal(wantB, got) {
				if len(wantB)+len(got) < 1000 {
					t.Logf(" got: %v\nwant: %v", got, wantB)
				} else {
					fileName, _ := filepath.Abs(filepath.Join("testdata", t.Name()+"-want.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err := ioutil.WriteFile(fileName, wantB, os.ModePerm)
					t.Log("Wrote file", fileName, err)

					fileName, _ = filepath.Abs(filepath.Join("testdata", t.Name()+"-got.bin"))
					_ = os.MkdirAll(filepath.Dir(fileName), os.ModePerm)
					err = ioutil.WriteFile(fileName, got, os.ModePerm)
					t.Log("Wrote file", fileName, err)
				}
				t.Logf("Length, want: %d, got: %d", len(wantB), len(got))
				t.Error("Output mismatch")
				return
			}
			t.Log(len(got), "bytes returned, matches input, ok!")
		})
	}
}

// Test our predefined tables are correct.
// We don't predefine them, since this also tests our transformations.
// Reference from here: https://github.com/facebook/zstd/blob/ededcfca57366461021c922720878c81a5854a0a/lib/decompress/zstd_decompress_block.c#L234
func TestPredefTables(t *testing.T) {
	for i := range fsePredef[:] {
		var want []decSymbol
		switch tableIndex(i) {
		case tableLiteralLengths:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 4, 0}, {16, 0, 4, 0},
				{32, 0, 5, 1}, {0, 0, 5, 3},
				{0, 0, 5, 4}, {0, 0, 5, 6},
				{0, 0, 5, 7}, {0, 0, 5, 9},
				{0, 0, 5, 10}, {0, 0, 5, 12},
				{0, 0, 6, 14}, {0, 1, 5, 16},
				{0, 1, 5, 20}, {0, 1, 5, 22},
				{0, 2, 5, 28}, {0, 3, 5, 32},
				{0, 4, 5, 48}, {32, 6, 5, 64},
				{0, 7, 5, 128}, {0, 8, 6, 256},
				{0, 10, 6, 1024}, {0, 12, 6, 4096},
				{32, 0, 4, 0}, {0, 0, 4, 1},
				{0, 0, 5, 2}, {32, 0, 5, 4},
				{0, 0, 5, 5}, {32, 0, 5, 7},
				{0, 0, 5, 8}, {32, 0, 5, 10},
				{0, 0, 5, 11}, {0, 0, 6, 13},
				{32, 1, 5, 16}, {0, 1, 5, 18},
				{32, 1, 5, 22}, {0, 2, 5, 24},
				{32, 3, 5, 32}, {0, 3, 5, 40},
				{0, 6, 4, 64}, {16, 6, 4, 64},
				{32, 7, 5, 128}, {0, 9, 6, 512},
				{0, 11, 6, 2048}, {48, 0, 4, 0},
				{16, 0, 4, 1}, {32, 0, 5, 2},
				{32, 0, 5, 3}, {32, 0, 5, 5},
				{32, 0, 5, 6}, {32, 0, 5, 8},
				{32, 0, 5, 9}, {32, 0, 5, 11},
				{32, 0, 5, 12}, {0, 0, 6, 15},
				{32, 1, 5, 18}, {32, 1, 5, 20},
				{32, 2, 5, 24}, {32, 2, 5, 28},
				{32, 3, 5, 40}, {32, 4, 5, 48},
				{0, 16, 6, 65536}, {0, 15, 6, 32768},
				{0, 14, 6, 16384}, {0, 13, 6, 8192}}
		case tableOffsets:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 5, 0}, {0, 6, 4, 61},
				{0, 9, 5, 509}, {0, 15, 5, 32765},
				{0, 21, 5, 2097149}, {0, 3, 5, 5},
				{0, 7, 4, 125}, {0, 12, 5, 4093},
				{0, 18, 5, 262141}, {0, 23, 5, 8388605},
				{0, 5, 5, 29}, {0, 8, 4, 253},
				{0, 14, 5, 16381}, {0, 20, 5, 1048573},
				{0, 2, 5, 1}, {16, 7, 4, 125},
				{0, 11, 5, 2045}, {0, 17, 5, 131069},
				{0, 22, 5, 4194301}, {0, 4, 5, 13},
				{16, 8, 4, 253}, {0, 13, 5, 8189},
				{0, 19, 5, 524285}, {0, 1, 5, 1},
				{16, 6, 4, 61}, {0, 10, 5, 1021},
				{0, 16, 5, 65533}, {0, 28, 5, 268435453},
				{0, 27, 5, 134217725}, {0, 26, 5, 67108861},
				{0, 25, 5, 33554429}, {0, 24, 5, 16777213}}
		case tableMatchLengths:
			want = []decSymbol{
				/* nextState, nbAddBits, nbBits, baseVal */
				{0, 0, 6, 3}, {0, 0, 4, 4},
				{32, 0, 5, 5}, {0, 0, 5, 6},
				{0, 0, 5, 8}, {0, 0, 5, 9},
				{0, 0, 5, 11}, {0, 0, 6, 13},
				{0, 0, 6, 16}, {0, 0, 6, 19},
				{0, 0, 6, 22}, {0, 0, 6, 25},
				{0, 0, 6, 28}, {0, 0, 6, 31},
				{0, 0, 6, 34}, {0, 1, 6, 37},
				{0, 1, 6, 41}, {0, 2, 6, 47},
				{0, 3, 6, 59}, {0, 4, 6, 83},
				{0, 7, 6, 131}, {0, 9, 6, 515},
				{16, 0, 4, 4}, {0, 0, 4, 5},
				{32, 0, 5, 6}, {0, 0, 5, 7},
				{32, 0, 5, 9}, {0, 0, 5, 10},
				{0, 0, 6, 12}, {0, 0, 6, 15},
				{0, 0, 6, 18}, {0, 0, 6, 21},
				{0, 0, 6, 24}, {0, 0, 6, 27},
				{0, 0, 6, 30}, {0, 0, 6, 33},
				{0, 1, 6, 35}, {0, 1, 6, 39},
				{0, 2, 6, 43}, {0, 3, 6, 51},
				{0, 4, 6, 67}, {0, 5, 6, 99},
				{0, 8, 6, 259}, {32, 0, 4, 4},
				{48, 0, 4, 4}, {16, 0, 4, 5},
				{32, 0, 5, 7}, {32, 0, 5, 8},
				{32, 0, 5, 10}, {32, 0, 5, 11},
				{0, 0, 6, 14}, {0, 0, 6, 17},
				{0, 0, 6, 20}, {0, 0, 6, 23},
				{0, 0, 6, 26}, {0, 0, 6, 29},
				{0, 0, 6, 32}, {0, 16, 6, 65539},
				{0, 15, 6, 32771}, {0, 14, 6, 16387},
				{0, 13, 6, 8195}, {0, 12, 6, 4099},
				{0, 11, 6, 2051}, {0, 10, 6, 1027},
			}
		}
		pre := fsePredef[i]
		got := pre.dt[:1<<pre.actualTableLog]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Predefined table %d incorrect, len(got) = %d, len(want) = %d", i, len(got), len(want))
		}
	}
}
