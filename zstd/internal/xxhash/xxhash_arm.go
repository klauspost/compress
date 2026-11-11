//go:build arm && arm.7 && gc && !purego && !noasm && !nounsafe

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
		v1 = round(v1, load64(unsafe.Add(p, i)))
		v2 = round(v2, load64(unsafe.Add(p, i+8)))
		v3 = round(v3, load64(unsafe.Add(p, i+16)))
		v4 = round(v4, load64(unsafe.Add(p, i+24)))
	}
	d.v1, d.v2, d.v3, d.v4 = v1, v2, v3, v4
	return n &^ 31
}

// load64 reads a little-endian uint64 with two 32-bit loads. The compiler
// lowers a uint64 load on arm32 to the same pair, but spelling it out makes it
// explicit that any input alignment is safe on pre-ARMv6 cores too.
func load64(p unsafe.Pointer) uint64 {
	lo := uint32(*(*uint32)(p))
	hi := uint32(*(*uint32)(unsafe.Add(p, 4)))
	return uint64(lo) | uint64(hi)<<32
}
