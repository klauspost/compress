package zstd

import "github.com/cespare/xxhash"

type Encoder struct {
	enc   simpleEncoder
	block *blockEnc
	crc   *xxhash.Digest
	tmp   [8]byte
}

func (e *Encoder) EncodeAll(src, dst []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	if e.block == nil {
		e.block = &blockEnc{}
		e.block.init()
	} else {
		e.block.initNewEncode()
	}
	e.enc.Reset()
	fh := frameHeader{
		ContentSize:   uint64(len(src)),
		WindowSize:    maxStoreBlockSize,
		SingleSegment: false,
		Checksum:      true,
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
		_, _ = e.crc.Write(todo)
		e.block.reset()
		e.block.pushOffsets()
		e.enc.Encode(e.block, todo)
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
	crc := e.crc.Sum(e.tmp[:0])
	dst = append(dst, crc[7], crc[6], crc[5], crc[4])
	return dst
}
