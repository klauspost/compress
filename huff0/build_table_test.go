package huff0

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"
)

func TestBuildCTableSimple(t *testing.T) {
	var count [256]uint32
	count['a'] = 60
	count['b'] = 30
	count['c'] = 10
	var s Scratch
	if err := s.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}
	if len(s.prevTable) < 4 {
		t.Fatalf("prevTable too short: %d", len(s.prevTable))
	}
	if s.prevTable['a'].nBits == 0 || s.prevTable['b'].nBits == 0 || s.prevTable['c'].nBits == 0 {
		t.Fatalf("expected non-zero nBits for active symbols")
	}
	// Symbols below the active set are zero in the trimmed prevTable.
	if s.prevTable[0].nBits != 0 {
		t.Fatalf("expected zero nBits for unused symbol 0")
	}
}

func TestBuildCTableEmpty(t *testing.T) {
	var count [256]uint32
	var s Scratch
	if err := s.BuildCTable(&count); err == nil {
		t.Fatalf("expected error for empty histogram")
	}
}

func TestBuildCTableNilCount(t *testing.T) {
	var s Scratch
	if err := s.BuildCTable(nil); err == nil {
		t.Fatalf("expected error for nil count")
	}
}

func TestEstimateSizeAndCanUseTableNilHist(t *testing.T) {
	var count [256]uint32
	count['a'] = 10
	count['b'] = 5
	var s Scratch
	if err := s.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}
	if got := s.EstimateSize(nil); got != -1 {
		t.Fatalf("EstimateSize(nil) = %d, want -1", got)
	}
	if s.CanUseTable(nil) {
		t.Fatalf("CanUseTable(nil) = true, want false")
	}
	// Also exercise the nil-receiver and unloaded-table paths so the guards
	// behave even before BuildCTable has installed prevTable.
	var empty Scratch
	if got := empty.EstimateSize(&count); got != -1 {
		t.Fatalf("EstimateSize on empty Scratch = %d, want -1", got)
	}
	if empty.CanUseTable(&count) {
		t.Fatalf("CanUseTable on empty Scratch = true, want false")
	}
}

func TestBuildCTableSingleSymbol(t *testing.T) {
	var count [256]uint32
	count[42] = 100
	var s Scratch
	if err := s.BuildCTable(&count); !errors.Is(err, ErrUseRLE) {
		t.Fatalf("expected ErrUseRLE, got %v", err)
	}
}

func TestBuildCTableEstimateAndCanUse(t *testing.T) {
	var count [256]uint32
	for i := range 32 {
		count[i] = uint32(32 - i) // skewed
	}
	var s Scratch
	if err := s.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}
	if !s.CanUseTable(&count) {
		t.Fatalf("CanUseTable: expected true for source histogram")
	}
	size := s.EstimateSize(&count)
	if size <= 0 {
		t.Fatalf("EstimateSize: expected positive, got %d", size)
	}
	var foreign [256]uint32
	foreign[200] = 10 // not in original
	if s.CanUseTable(&foreign) {
		t.Fatalf("CanUseTable: expected false for foreign histogram")
	}
	if s.EstimateSize(&foreign) >= 0 {
		t.Fatalf("EstimateSize: expected -1 for foreign histogram")
	}
}

// TestBuildCTableRoundtrip builds a table from a histogram, then encodes data
// with that prebuilt table (ReusePolicyMust) and verifies the decoder
// reconstructs the input via the standard table-in-stream format.
func TestBuildCTableRoundtrip(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	// Generate skewed input bytes (so compression yields meaningful tables).
	input := make([]byte, 16<<10)
	for i := range input {
		// 80% zero, 20% in [1,9]
		if rng.Intn(5) == 0 {
			input[i] = byte(1 + rng.Intn(9))
		}
	}
	var count [256]uint32
	for _, b := range input {
		count[b]++
	}

	var src Scratch
	if err := src.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}

	// Hand-off the prebuilt table to the encoder via prevTable + ReusePolicyMust.
	var enc Scratch
	enc.TransferCTable(&src)
	enc.Reuse = ReusePolicyMust

	out, reused, err := Compress4X(input, &enc)
	if err != nil {
		t.Fatalf("Compress4X: %v", err)
	}
	if !reused {
		t.Fatalf("expected table reuse")
	}
	if len(enc.OutTable) != 0 {
		t.Fatalf("expected no table emitted on reuse, got %d bytes", len(enc.OutTable))
	}

	// To verify decompression we have to ship the table to the decoder.
	// Encode once more without ReusePolicy to obtain the serialized table,
	// then concatenate that header with the reuse-only data above to mimic
	// a stream that starts with the table.
	var noReuse Scratch
	noReuse.Reuse = ReusePolicyNone
	if _, _, err := Compress4X(input, &noReuse); err != nil {
		t.Fatalf("Compress4X (no reuse): %v", err)
	}
	if len(noReuse.OutTable) == 0 {
		t.Fatalf("expected non-empty OutTable on non-reuse")
	}

	// Read a decoder-side scratch from the table header.
	var dec Scratch
	dec.MaxDecodedSize = len(input)
	dec2, rem, err := ReadTable(noReuse.OutTable, &dec)
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(rem) != 0 {
		t.Fatalf("expected no remainder, got %d", len(rem))
	}

	got, err := dec2.Decompress4X(out, len(input))
	if err != nil {
		t.Fatalf("Decompress4X: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("roundtrip mismatch: got len=%d want len=%d", len(got), len(input))
	}
}

// TestAppendTableRoundtrip verifies AppendTable produces a header that
// ReadTable can parse, restoring an equivalent table.
func TestAppendTableRoundtrip(t *testing.T) {
	var count [256]uint32
	rng := rand.New(rand.NewSource(99))
	for i := range 64 {
		count[i] = uint32(1 + rng.Intn(1000))
	}
	var src Scratch
	if err := src.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}
	hdr, err := src.AppendTable(nil)
	if err != nil {
		t.Fatalf("AppendTable: %v", err)
	}
	if len(hdr) == 0 {
		t.Fatalf("AppendTable produced empty header")
	}
	var dst Scratch
	_, rem, err := ReadTable(hdr, &dst)
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(rem) != 0 {
		t.Fatalf("expected no remainder, got %d bytes", len(rem))
	}
	if dst.prevTableLog != src.prevTableLog {
		t.Fatalf("prevTableLog mismatch: src=%d dst=%d", src.prevTableLog, dst.prevTableLog)
	}
	for i := range count {
		if count[i] == 0 {
			continue
		}
		if i >= len(dst.prevTable) || dst.prevTable[i].nBits == 0 {
			t.Fatalf("missing symbol %d in roundtripped table", i)
		}
	}
}

// TestBuildCTableMatchesCompress verifies that BuildCTable followed by reuse
// produces output identical to the table embedded by a regular Compress4X
// call on the same input (modulo the table header).
func TestBuildCTableMatchesCompress(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	input := make([]byte, 8<<10)
	for i := range input {
		input[i] = byte(rng.Intn(16))
	}
	var ref Scratch
	ref.Reuse = ReusePolicyNone
	refOut, refReused, err := Compress4X(input, &ref)
	if err != nil {
		t.Fatalf("ref Compress4X: %v", err)
	}
	if refReused {
		t.Fatalf("ref should not reuse")
	}
	refData := refOut[len(ref.OutTable):]

	var count [256]uint32
	for _, b := range input {
		count[b]++
	}
	var built Scratch
	if err := built.BuildCTable(&count); err != nil {
		t.Fatalf("BuildCTable: %v", err)
	}
	var enc Scratch
	enc.TransferCTable(&built)
	enc.Reuse = ReusePolicyMust
	out, reused, err := Compress4X(input, &enc)
	if err != nil {
		t.Fatalf("Compress4X: %v", err)
	}
	if !reused {
		t.Fatalf("expected reuse")
	}
	if !bytes.Equal(out, refData) {
		t.Fatalf("compressed payload mismatch: built=%d ref=%d", len(out), len(refData))
	}
}
