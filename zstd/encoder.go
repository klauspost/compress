// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"io"
)

// Encoder provides encoding to Zstandard
type Encoder struct {
	o        encoderOptions
	encoders chan *fastEncoder
	blocks   chan *blockEnc
}

// NewWriter will create a new Zstandard encoder.
// If the encoder will be used for encoding blocks a nil writer can be used.
func NewWriter(w io.Writer, opts ...EOption) (*Encoder, error) {
	var e Encoder
	e.o.setDefault()
	for _, o := range opts {
		err := o(&e.o)
		if err != nil {
			return nil, err
		}
	}
	e.encoders = make(chan *fastEncoder, e.o.concurrent)
	e.blocks = make(chan *blockEnc, e.o.concurrent)
	for i := 0; i < e.o.concurrent; i++ {
		enc := fastEncoder{}
		blk := blockEnc{}
		blk.init()
		e.encoders <- &enc
		e.blocks <- &blk
	}
	return &e, nil
}

// EncodeAll will encode all input in src and append it to dst.
// This function can be called concurrently, but each call will only run on a single goroutine.
// If empty input is given, nothing is returned.
func (e *Encoder) EncodeAll(src, dst []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	blk := <-e.blocks
	enc := <-e.encoders
	defer func() {
		// Release encoder reference to last block.
		enc.Reset()
		e.blocks <- blk
		e.encoders <- enc
	}()
	enc.Reset()
	blk.initNewEncode()
	fh := frameHeader{
		ContentSize:   uint64(len(src)),
		WindowSize:    maxStoreBlockSize * 2,
		SingleSegment: false,
		Checksum:      e.o.crc,
		DictID:        0,
	}
	dst, err := fh.appendTo(dst)
	if err != nil {
		panic(err)
	}

	for len(src) > 0 {
		todo := src
		if len(todo) > maxStoreBlockSize {
			todo = todo[:maxStoreBlockSize]
		}
		src = src[len(todo):]
		if e.o.crc {
			_, _ = enc.crc.Write(todo)
		}
		blk.reset()
		blk.pushOffsets()
		enc.Encode(blk, todo)
		if len(src) == 0 {
			blk.last = true
		}
		err := blk.encode()
		switch err {
		case errIncompressible:
			if debug {
				println("Storing uncompressible block as raw")
			}
			blk.encodeRaw(todo)
			blk.popOffsets()
		case nil:
		default:
			panic(err)
		}
		dst = append(dst, blk.output...)
	}
	if e.o.crc {
		crc := enc.crc.Sum(enc.tmp[:0])
		dst = append(dst, crc[7], crc[6], crc[5], crc[4])
	}
	enc.Reset()
	return dst
}
