//go:build arm && !appengine && gc && !purego && !noasm

package xxhash

import "unsafe"

// writeBlocks is the arm32 fast path for the 32-byte stripe loop. The generic
// version indexes b[0:8:len(b)] four times per stripe and reloads the slice
// bounds each time; on arm32 those bounds checks and the slice arithmetic cost
// more than the UMULL sequence the compiler emits for the 64-bit multiplies.
// Loading through an unsafe pointer keeps the four accumulators live across
// the whole loop.
func writeBlocks(d *Digest, b []byte) int {
	n := len(b)
	if n < 32 {
		return 0
	}
	p := unsafe.Pointer(unsafe.SliceData(b))
	v1, v2, v3, v4 := d.v1, d.v2, d.v3, d.v4
	// Overread to the last full stripe is avoided; the caller handles the tail.
	for i := 0; i+32 <= n; i += 32 {
		v1 = round(v1, *(*uint64)(unsafe.Add(p, i)))
		v2 = round(v2, *(*uint64)(unsafe.Add(p, i+8)))
		v3 = round(v3, *(*uint64)(unsafe.Add(p, i+16)))
		v4 = round(v4, *(*uint64)(unsafe.Add(p, i+24)))
	}
	d.v1, d.v2, d.v3, d.v4 = v1, v2, v3, v4
	return n &^ 31
}
