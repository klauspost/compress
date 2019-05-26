package zstd

import "github.com/cespare/xxhash"

// Encoder provides encoding to Zstandard
type Encoder struct {
	Crc bool

	enc   simpleEncoder
	block *blockEnc
	crc   *xxhash.Digest
	tmp   [8]byte
}

// EncodeAll will encode all input in src and append it to dst.
// If empty input is given, nothing is returned.
func (e *Encoder) EncodeAll(src, dst []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	if e.block == nil {
		e.block = &blockEnc{}
		e.block.init()
	}
	e.block.initNewEncode()
	e.enc.Reset()
	fh := frameHeader{
		ContentSize:   uint64(len(src)),
		WindowSize:    maxStoreBlockSize * 2,
		SingleSegment: false,
		Checksum:      e.Crc,
		DictID:        0,
	}
	dst, err := fh.appendTo(dst)
	if err != nil {
		panic(err)
	}
	if e.crc == nil {
		e.crc = xxhash.New()
	} else {
		e.crc.Reset()
	}
	for len(src) > 0 {
		todo := src
		if len(todo) > maxStoreBlockSize {
			todo = todo[:maxStoreBlockSize]
		}
		src = src[len(todo):]
		if e.Crc {
			_, _ = e.crc.Write(todo)
		}
		e.block.reset()
		e.block.pushOffsets()
		e.enc.EncodeFast(e.block, todo)
		if len(src) == 0 {
			e.block.last = true
		}
		err := e.block.encode()
		switch err {
		case errIncompressible:
			println("Storing uncompressible block as raw")
			e.block.encodeRaw(todo)
			e.block.popOffsets()
		case nil:
		default:
			panic(err)
		}
		dst = append(dst, e.block.output...)
	}
	if e.Crc {
		crc := e.crc.Sum(e.tmp[:0])
		dst = append(dst, crc[7], crc[6], crc[5], crc[4])
	}
	e.enc.Reset()
	return dst
}
