// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"io"
	"sync"
)

// Encoder provides encoding to Zstandard
type Encoder struct {
	o        encoderOptions
	encoders chan *fastEncoder
	state    encoderState
	init     sync.Once
}

type encoderState struct {
	w             io.Writer
	filling       []byte
	current       []byte
	previous      []byte
	encoder       *fastEncoder
	err           error
	headerWritten bool
	eofWritten    bool

	// This waitgroup indicates an encode is running.
	wg sync.WaitGroup
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
	if w != nil {
		e.Reset(w)
	}
	e.initialize()
	return &e, nil
}

func (e *Encoder) initialize() {
	e.encoders = make(chan *fastEncoder, e.o.concurrent)
	for i := 0; i < e.o.concurrent; i++ {
		enc := fastEncoder{}
		e.encoders <- &enc
	}
}

func (e *Encoder) Reset(w io.Writer) {
	s := &e.state
	s.wg.Wait()
	if cap(s.filling) == 0 {
		s.filling = make([]byte, 0, maxStoreBlockSize)
	}
	if cap(s.current) == 0 {
		s.current = make([]byte, 0, maxStoreBlockSize)
	}
	if cap(s.previous) == 0 {
		s.previous = make([]byte, 0, maxStoreBlockSize)
	}
	if s.encoder == nil {
		s.encoder = &fastEncoder{}
	}
	s.filling = s.filling[:0]
	s.current = s.current[:0]
	s.previous = s.previous[:0]
	s.encoder.Reset()
	s.headerWritten = false
	s.eofWritten = false
	s.w = w
	s.err = nil
}

func (e *Encoder) Write(p []byte) (n int, err error) {
	s := &e.state
	for len(p) > 0 {
		if len(p)+len(s.filling) < maxStoreBlockSize {
			if e.o.crc {
				_, _ = s.encoder.crc.Write(p)
			}
			s.filling = append(s.filling, p...)
			return n + len(p), nil
		}
		add := p[:maxStoreBlockSize]
		if e.o.crc {
			_, _ = s.encoder.crc.Write(add)
		}
		s.filling = append(s.filling, add...)
		p = p[len(add):]
		n += len(add)
		if len(s.filling) < maxStoreBlockSize {
			return n, nil
		}
		err := e.nextBlock(false)
		if err != nil {
			return n, err
		}
		if debug && len(s.filling) > 0 {
			panic(len(s.filling))
		}
	}
	return n, nil
}

func (e *Encoder) nextBlock(final bool) error {
	s := &e.state
	// Wait for current block.
	s.wg.Wait()
	if s.err != nil {
		return s.err
	}
	if !s.headerWritten {
		var tmp [maxHeaderSize]byte
		fh := frameHeader{
			ContentSize:   0,
			WindowSize:    maxStoreBlockSize * 2,
			SingleSegment: false,
			Checksum:      e.o.crc,
			DictID:        0,
		}
		dst, err := fh.appendTo(tmp[:0])
		if err != nil {
			return err
		}
		s.headerWritten = true
		_, s.err = s.w.Write(dst)
		if s.err != nil {
			return s.err
		}
	}
	if s.eofWritten {
		// Ensure we only write it once.
		final = false
	}

	if len(s.filling) == 0 {
		// Final block, but no data.
		if final {
			enc := s.encoder
			blk := enc.blk
			blk.reset()
			blk.last = true
			blk.encodeRaw(nil)
			_, s.err = s.w.Write(blk.output)
		}
		return s.err
	}

	// Move blocks forward.
	s.filling, s.current, s.previous = s.previous[:0], s.filling, s.current
	s.wg.Add(1)
	go func(src []byte) {
		if debug {
			println("Adding block,", len(src), "bytes, final:", final)
		}
		defer s.wg.Done()
		enc := s.encoder
		blk := enc.blk
		blk.reset()
		blk.pushOffsets()
		enc.Encode(blk, src)
		blk.last = final
		if final {
			s.eofWritten = true
		}
		err := blk.encode()
		switch err {
		case errIncompressible:
			if debug {
				println("Storing incompressible block as raw")
			}
			blk.encodeRaw(src)
			blk.popOffsets()
		case nil:
		default:
			s.err = err
			return
		}
		_, s.err = s.w.Write(blk.output)
	}(s.current)
	return nil
}

// ReadFrom reads data from r until EOF or error.
// The return value n is the number of bytes read.
// Any error except io.EOF encountered during the read is also returned.
//
// The Copy function uses ReaderFrom if available.
func (e *Encoder) ReadFrom(r io.Reader) (n int64, err error) {
	if debug {
		println("Using ReadFrom")
	}
	// Maybe handle stuff queued?
	e.state.filling = e.state.filling[:maxStoreBlockSize]
	src := e.state.filling
	for {
		n2, err := r.Read(src)
		_, _ = e.state.encoder.crc.Write(src[:n2])
		// src is now the unfilled part...
		src = src[n2:]
		n += int64(n2)
		switch err {
		case io.EOF:
			e.state.filling = e.state.filling[:len(e.state.filling)-len(src)]
			if debug {
				println("ReadFrom: got EOF final block:", len(e.state.filling))
			}
			return n, e.nextBlock(true)
		default:
			if debug {
				println("ReadFrom: got error:", err)
			}
			e.state.err = err
			return n, err
		case nil:
		}
		if len(src) > 0 {
			if debug {
				println("ReadFrom: got space left in source:", len(src))
			}
			continue
		}
		err = e.nextBlock(false)
		if err != nil {
			return n, err
		}
		e.state.filling = e.state.filling[:maxStoreBlockSize]
		src = e.state.filling
	}
}

// Flush will send the currently written data to output
// and block until everything has been written.
// This should only be used on rare occasions where pushing the currently queued data is critical.
func (e *Encoder) Flush() error {
	s := &e.state
	if len(s.filling) > 0 {
		err := e.nextBlock(false)
		if err != nil {
			return err
		}
	}
	s.wg.Wait()
	return s.err
}

// Close will flush the final output and close the stream.
// The function will block until everything has been written.
func (e *Encoder) Close() error {
	s := &e.state
	if s.encoder == nil {
		return nil
	}
	err := e.nextBlock(true)
	if err != nil {
		return err
	}
	s.wg.Wait()

	// Write CRC
	if e.o.crc && s.err == nil {
		crc := s.encoder.crc.Sum(s.encoder.tmp[:0])
		crc[0], crc[1], crc[2], crc[3] = crc[7], crc[6], crc[5], crc[4]
		_, s.err = s.w.Write(crc[:4])
	}
	return s.err
}

// EncodeAll will encode all input in src and append it to dst.
// This function can be called concurrently, but each call will only run on a single goroutine.
// If empty input is given, nothing is returned.
// Encoded blocks can be concatenated
func (e *Encoder) EncodeAll(src, dst []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	e.init.Do(func() {
		e.o.setDefault()
		e.initialize()
	})
	enc := <-e.encoders
	defer func() {
		// Release encoder reference to last block.
		enc.Reset()
		e.encoders <- enc
	}()
	enc.Reset()
	blk := enc.blk
	fh := frameHeader{
		ContentSize:   uint64(len(src)),
		WindowSize:    maxStoreBlockSize * 2,
		SingleSegment: e.o.single,
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
	return dst
}
