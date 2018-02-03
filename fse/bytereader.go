package fse

type byteReader struct {
	b   []byte
	off int
}

func (b *byteReader) advance(n uint) {
	b.off += int(n)
}

func (b byteReader) Int32() int32 {
	v3 := int32(b.b[b.off+3])
	v2 := int32(b.b[b.off+2])
	v1 := int32(b.b[b.off+1])
	v0 := int32(b.b[b.off])
	return (v3 << 24) | (v2 << 16) | (v1 << 8) | v0
}

func (b byteReader) Uint32() uint32 {
	v3 := uint32(b.b[b.off+3])
	v2 := uint32(b.b[b.off+2])
	v1 := uint32(b.b[b.off+1])
	v0 := uint32(b.b[b.off])
	return (v3 << 24) | (v2 << 16) | (v1 << 8) | v0
}

// unread() returns the unread portion of the input.
func (b byteReader) unread() []byte {
	return b.b[b.off:]
}

func (b byteReader) remain() int {
	return len(b.b) - b.off
}
