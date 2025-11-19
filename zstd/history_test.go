package zstd

import "testing"

func TestHistoryEnsureBlockShrinkLargeExcess(t *testing.T) {
	const (
		initialTarget = 8 << 20
		finalTarget   = 1 << 20
	)

	var h history
	h.windowSize = maxCompressedBlockSize
	h.allocFrameBuffer = initialTarget
	h.ensureBlock()
	if cap(h.b) != initialTarget {
		t.Fatalf("initial ensureBlock cap = %d, want %d", cap(h.b), initialTarget)
	}

	h.allocFrameBuffer = finalTarget
	h.ensureBlock()
	if cap(h.b) != finalTarget {
		t.Fatalf("shrink ensureBlock cap = %d, want %d", cap(h.b), finalTarget)
	}
	if len(h.b) != 0 {
		t.Fatalf("shrink ensureBlock len = %d, want 0", len(h.b))
	}
}

func TestHistoryEnsureBlockShrinkWithinLimit(t *testing.T) {
	const (
		initialTarget = 2 << 20
		smallerTarget = 3 << 19 // 1.5 MB, diff < smallerTarget/2
	)

	var h history
	h.windowSize = maxCompressedBlockSize
	h.allocFrameBuffer = initialTarget
	h.ensureBlock()
	if cap(h.b) != initialTarget {
		t.Fatalf("initial ensureBlock cap = %d, want %d", cap(h.b), initialTarget)
	}

	h.allocFrameBuffer = smallerTarget
	h.ensureBlock()
	if cap(h.b) != initialTarget {
		t.Fatalf("ensureBlock should keep cap %d, got %d", initialTarget, cap(h.b))
	}
	if len(h.b) != 0 {
		t.Fatalf("ensureBlock len = %d, want 0", len(h.b))
	}
}

func TestHistoryEnsureBlockGrowPreservesData(t *testing.T) {
	data := make([]byte, 256, 512)
	for i := range data {
		data[i] = byte(i)
	}

	var h history
	h.windowSize = maxCompressedBlockSize
	h.b = data
	h.allocFrameBuffer = 1024

	before := append([]byte(nil), h.b...)
	h.ensureBlock()

	if cap(h.b) != h.allocFrameBuffer {
		t.Fatalf("ensureBlock grow cap = %d, want %d", cap(h.b), h.allocFrameBuffer)
	}
	if len(h.b) != len(before) {
		t.Fatalf("ensureBlock grow len = %d, want %d", len(h.b), len(before))
	}
	for i := range before {
		if h.b[i] != before[i] {
			t.Fatalf("ensureBlock grow data mismatch at %d: got %d, want %d", i, h.b[i], before[i])
		}
	}
}

func BenchmarkHistoryEnsureBlockShrink(b *testing.B) {
	const (
		initialTarget = 8 << 20 // 8 MB
		finalTarget   = 1 << 20 // 1 MB
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var h history
		h.windowSize = maxCompressedBlockSize
		h.allocFrameBuffer = initialTarget
		h.ensureBlock()

		h.allocFrameBuffer = finalTarget
		h.ensureBlock()
	}
}

func BenchmarkHistoryEnsureBlockGrowWithData(b *testing.B) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var h history
		h.windowSize = maxCompressedBlockSize
		h.b = make([]byte, len(data), 512)
		copy(h.b, data)
		h.allocFrameBuffer = 1024

		h.ensureBlock()
	}
}
