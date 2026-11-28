package xxhash

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAll(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  uint64
	}{
		{"empty", "", 0xef46db3751d8e999},
		{"a", "a", 0xd24ec4f1a98c6e5b},
		{"as", "as", 0x1c330fb2d66be179},
		{"asd", "asd", 0x631c37ce72a97393},
		{"asdf", "asdf", 0x415872f599cea71e},
		{
			"len=63",
			// Exactly 63 characters, which exercises all code paths.
			"Call me Ishmael. Some years ago--never mind how long precisely-",
			0x02a2e85470d6fd96,
		},
	} {
		lastChunkSize := len(tt.input)
		if lastChunkSize == 0 {
			lastChunkSize = 1
		}
		for chunkSize := 1; chunkSize <= lastChunkSize; chunkSize++ {
			name := fmt.Sprintf("%s,chunkSize=%d", tt.name, chunkSize)
			t.Run(name, func(t *testing.T) {
				testDigest(t, tt.input, chunkSize, tt.want)
			})
		}
		t.Run(tt.name, func(t *testing.T) { testSum(t, tt.input, tt.want) })
	}
}

func testDigest(t *testing.T, input string, chunkSize int, want uint64) {
	d := New()
	ds := New() // uses WriteString
	for i := 0; i < len(input); i += chunkSize {
		chunk := input[i:]
		if len(chunk) > chunkSize {
			chunk = chunk[:chunkSize]
		}
		n, err := d.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Digest.Write: got (%d, %v); want (%d, nil)", n, err, len(chunk))
		}
		n, err = ds.WriteString(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("Digest.WriteString: got (%d, %v); want (%d, nil)", n, err, len(chunk))
		}
	}
	if got := d.Sum64(); got != want {
		t.Fatalf("Digest.Sum64: got 0x%x; want 0x%x", got, want)
	}
	if got := ds.Sum64(); got != want {
		t.Fatalf("Digest.Sum64 (WriteString): got 0x%x; want 0x%x", got, want)
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], want)
	if got := d.Sum(nil); !bytes.Equal(got, b[:]) {
		t.Fatalf("Sum: got %v; want %v", got, b[:])
	}
}

func testSum(t *testing.T, input string, want uint64) {
	if got := Sum64([]byte(input)); got != want {
		t.Fatalf("Sum64: got 0x%x; want 0x%x", got, want)
	}
	if got := Sum64String(input); got != want {
		t.Fatalf("Sum64String: got 0x%x; want 0x%x", got, want)
	}
}

func TestWriteAlignment(t *testing.T) {
	for _, size := range []int{31, 32, 33, 63, 64, 65, 1025} {
		input := make([]byte, size)
		for i := range input {
			input[i] = byte(i*37 + i/7)
		}
		want := Sum64(input)
		for offset := range 8 {
			t.Run(fmt.Sprintf("size=%d/offset=%d", size, offset), func(t *testing.T) {
				buf := make([]byte, offset+size)[offset:]
				copy(buf, input)
				d := New()
				if n, err := d.Write(buf); err != nil || n != size {
					t.Fatalf("Write: n=%d err=%v", n, err)
				}
				if got := d.Sum64(); got != want {
					t.Fatalf("Sum64: got %x, want %x", got, want)
				}
			})
		}
	}
}

func TestReset(t *testing.T) {
	parts := []string{"The quic", "k br", "o", "wn fox jumps", " ov", "er the lazy ", "dog."}
	d := New()
	for _, part := range parts {
		d.Write([]byte(part))
	}
	h0 := d.Sum64()

	d.Reset()
	d.Write([]byte(strings.Join(parts, "")))
	h1 := d.Sum64()

	if h0 != h1 {
		t.Errorf("0x%x != 0x%x", h0, h1)
	}
}

func TestBinaryMarshaling(t *testing.T) {
	d := New()
	d.WriteString("abc")
	b, err := d.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	d = New()
	d.WriteString("junk")
	if err := d.UnmarshalBinary(b); err != nil {
		t.Fatal(err)
	}
	d.WriteString("def")
	if got, want := d.Sum64(), Sum64String("abcdef"); got != want {
		t.Fatalf("after MarshalBinary+UnmarshalBinary, got 0x%x; want 0x%x", got, want)
	}

	d0 := New()
	d1 := New()
	for i := range 64 {
		b, err := d0.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		d0 = new(Digest)
		if err := d0.UnmarshalBinary(b); err != nil {
			t.Fatal(err)
		}
		if got, want := d0.Sum64(), d1.Sum64(); got != want {
			t.Fatalf("after %d Writes, unmarshaled Digest gave sum 0x%x; want 0x%x", i, got, want)
		}

		d0.Write([]byte{'a'})
		d1.Write([]byte{'a'})
	}
}

var sink uint64

func TestAllocs(t *testing.T) {
	const shortStr = "abcdefghijklmnop"
	// Sum64([]byte(shortString)) shouldn't allocate because the
	// intermediate []byte ought not to escape.
	// (See https://github.com/cespare/xxhash/pull/2.)
	t.Run("Sum64", func(t *testing.T) {
		testAllocs(t, func() {
			sink = Sum64([]byte(shortStr))
		})
	})
	// Creating and using a Digest shouldn't allocate because its methods
	// shouldn't make it escape. (A previous version of New returned a
	// hash.Hash64 which forces an allocation.)
	t.Run("Digest", func(t *testing.T) {
		b := []byte("asdf")
		testAllocs(t, func() {
			d := New()
			d.Write(b)
			sink = d.Sum64()
		})
	})
}

func testAllocs(t *testing.T, fn func()) {
	t.Helper()
	if allocs := int(testing.AllocsPerRun(10, fn)); allocs > 0 {
		t.Fatalf("got %d allocation(s) (want zero)", allocs)
	}
}

// gcWait collects while work runs in another goroutine, and reports the longest
// a GC had to wait along with how long the work itself took. Work the runtime
// cannot preempt shows up as a wait that is a large fraction of the work.
func gcWait(work func()) (worst, elapsed time.Duration) {
	done := make(chan struct{})
	begin := time.Now()
	go func() { defer close(done); work() }()
	for {
		select {
		case <-done:
			return worst, time.Since(begin)
		default:
		}
		start := time.Now()
		runtime.GC()
		worst = max(worst, time.Since(start))
	}
}

// TestDigestIsPreemptible fails on two counts, so that neither a fast machine
// nor a loaded one decides the outcome. Both bars are relative, never absolute:
// how long a GC waited against the hash it overlapped, and against a plain Go
// loop, which is always preemptible and so gives this runner's noise floor.
func TestDigestIsPreemptible(t *testing.T) {
	if testing.Short() || runtime.GOMAXPROCS(0) < 2 {
		t.Skip("hashes 64 MiB, and needs GOMAXPROCS >= 2 to see a stalled GC")
	}
	// One Write, so an unbounded writeBlocks is a single call and the GC waits
	// out the whole hash rather than one chunk of it.
	buf := make([]byte, 64*1024*1024)

	// Three rounds each, keeping the reading that most favours a pass: the best
	// hash and the worst plain-Go loop. A loaded runner can stall any single
	// measurement, but only unpreemptible code stalls every one of them.
	got, hash := time.Duration(math.MaxInt64), time.Duration(0)
	var floor time.Duration
	for range 3 {
		w, _ := gcWait(func() {
			for _, b := range buf {
				sink += uint64(b)
			}
		})
		floor = max(floor, w)

		w, e := gcWait(func() {
			var d Digest
			d.Reset()
			d.Write(buf)
			sink = d.Sum64()
		})
		if w < got {
			got, hash = w, e
		}
	}

	t.Logf("worst GC wait: %v of a %v Digest.Write, against %v on a plain Go loop", got, hash, floor)
	if got > hash/4 && got > 4*floor {
		t.Fatalf("a GC waited %v of a %v Digest.Write, against %v on a plain Go "+
			"loop: writeBlocks is running unbounded", got, hash, floor)
	}
}
