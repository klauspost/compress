//go:build (!amd64 && !arm64 && !arm) || !gc || purego || noasm

package xxhash

func writeBlocks(d *Digest, b []byte) int {
	v1, v2, v3, v4 := d.v1, d.v2, d.v3, d.v4
	n := len(b)
	for len(b) >= 32 {
		v1 = round(v1, u64(b[0:8:len(b)]))
		v2 = round(v2, u64(b[8:16:len(b)]))
		v3 = round(v3, u64(b[16:24:len(b)]))
		v4 = round(v4, u64(b[24:32:len(b)]))
		b = b[32:len(b):len(b)]
	}
	d.v1, d.v2, d.v3, d.v4 = v1, v2, v3, v4
	return n - len(b)
}
