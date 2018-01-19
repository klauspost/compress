package fse

type byteReader struct {
	b   []byte
	off int
}

func (b byteReader) Uint32() uint32 {
	v3 := uint32(b.b[b.off+3])
	v2 := uint32(b.b[b.off+2])
	v1 := uint32(b.b[b.off+1])
	v0 := uint32(b.b[b.off])
	b.off += 4
	return (v3 << 24) | (v2 << 16) | (v1 << 8) | v0
}

// Uint32Back2 will go back 2 and ready 4 bytes.
func (b byteReader) Uint32Back2() uint32 {
	v3 := uint32(b.b[b.off+1])
	v2 := uint32(b.b[b.off+0])
	v1 := uint32(b.b[b.off-1])
	v0 := uint32(b.b[b.off-2])
	b.off += 2
	return (v3 << 24) | (v2 << 16) | (v1 << 8) | v0
}

// Uint32BackN will go back n bytes and ready 4 bytes.
func (b byteReader) Uint32BackN(n int) uint32 {
	v3 := uint32(b.b[b.off+3-n])
	v2 := uint32(b.b[b.off+2-n])
	v1 := uint32(b.b[b.off+1-n])
	v0 := uint32(b.b[b.off+2-n])
	b.off += 4 - n
	return (v3 << 24) | (v2 << 16) | (v1 << 8) | v0
}

func (b byteReader) Low16Uint32() uint32 {
	v1 := uint32(b.b[b.off+1])
	v0 := uint32(b.b[b.off])
	b.off += 2
	return (v1 << 8) | v0
}

func (b byteReader) remain() int {
	return len(b.b) - b.off
}
