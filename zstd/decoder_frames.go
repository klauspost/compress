// Copyright 2026+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.

package zstd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"slices"
)

// frameResult is the output of one frame decoded by DecodeAll.
type frameResult struct {
	b   []byte
	err error
}

// startFrameDecoder splits r at frame boundaries and decodes up to
// o.concurrent frames at once with DecodeAll, sending each frame's output to
// output in order. From the first frame that cannot be decoded this way, it
// decodes the rest of r with startStreamDecoder, which also reports any error.
func (d *Decoder) startFrameDecoder(ctx context.Context, r io.Reader, output chan decodeOutput, o decoderOptions) {
	handedOff := false
	defer func() {
		if !handedOff {
			d.streamWg.Done()
		}
	}()
	limit := min(o.frameConcurrentMax, o.maxDecodedSize)
	maxWindow := d.frame.o.maxWindowSize
	// Decode with the stream's options; the cap limit is for DecodeAll calls.
	o.limitToCap = false

	// DecodeAll waits for a free block decoder, which bounds the frames
	// decoded at once. pending bounds how far reading runs ahead.
	pending := make(chan chan frameResult, o.concurrent)
	fwdDone := make(chan struct{})
	go func() {
		defer close(fwdDone)
		for ch := range pending {
			// Always wait, so no DecodeAll outlives the stream.
			res := <-ch
			select {
			case output <- decodeOutput{b: res.b, err: res.err}:
			case <-ctx.Done():
			}
		}
	}()

	// After an error, the reader cancels ctx and drains output.
	var tail io.Reader
	for ctx.Err() == nil {
		frame, size, ok, err := readParallelFrame(r, d.getFrameIn(), limit, maxWindow)
		if err == io.EOF {
			break
		}
		if !ok {
			tail = io.MultiReader(bytes.NewReader(frame), errorOr(r, err))
			break
		}
		ch := make(chan frameResult, 1)
		pending <- ch
		dst := d.getFrameBuf(size)
		go func() {
			b, err := d.decodeAll(frame, dst, o)
			d.putFrameIn(frame)
			if err != nil {
				// Partial output may include part of the failing block.
				b = nil
			}
			ch <- frameResult{b: b, err: err}
		}()
	}
	close(pending)
	<-fwdDone

	switch {
	case ctx.Err() != nil:
		close(output)
	case tail != nil:
		handedOff = true
		d.startStreamDecoder(ctx, tail, output)
	default:
		output <- decodeOutput{err: io.EOF}
		close(output)
	}
}

// getFrameBuf returns a buffer the reader has finished with if it holds a
// frame of size bytes, or nil to let DecodeAll allocate.
func (d *Decoder) getFrameBuf(size uint64) []byte {
	select {
	case b := <-d.frameBufs:
		if size == fcsUnknown || uint64(cap(b)) >= size+compressedBlockOverAlloc {
			return b[:0]
		}
	default:
	}
	return nil
}

// putFrameBuf keeps b for reuse by getFrameBuf.
func (d *Decoder) putFrameBuf(b []byte) {
	if cap(b) == 0 {
		return
	}
	select {
	case d.frameBufs <- b:
	default:
	}
}

// getFrameIn returns an empty buffer for a compressed frame.
func (d *Decoder) getFrameIn() []byte {
	select {
	case b := <-d.frameIn:
		return b[:0]
	default:
		return nil
	}
}

// putFrameIn keeps b for reuse by getFrameIn.
func (d *Decoder) putFrameIn(b []byte) {
	select {
	case d.frameIn <- b:
	default:
	}
}

// readParallelFrame reads the next frame from r, appending it to buf and
// skipping skippable frames. It returns the frame and its content size, or
// fcsUnknown, with ok set if it decodes to at most limit bytes and has a window
// of at most maxWindow. Otherwise it returns ok=false with the bytes read so far
// and any read error, and the caller decodes the rest of the stream serially,
// so that decoder reports any problem with the frame. At a clean end of r it
// returns io.EOF.
func readParallelFrame(r io.Reader, buf []byte, limit, maxWindow uint64) (frame []byte, size uint64, ok bool, err error) {
	defer func() {
		// Only an end of r between frames is clean.
		if err == io.EOF && frame != nil {
			err = io.ErrUnexpectedEOF
		}
	}()
	// Magic, the largest frame header and a block header.
	var hdr [4 + 14 + 3]byte
	for {
		if n, err := io.ReadFull(r, hdr[:4]); err != nil {
			if err == io.EOF {
				return nil, 0, false, io.EOF
			}
			return append(buf, hdr[:n]...), 0, false, err
		}
		if string(hdr[:4]) == frameMagic {
			break
		}
		if string(hdr[1:4]) != skippableFrameMagic || hdr[0]&0xf0 != 0x50 {
			return append(buf, hdr[:4]...), 0, false, nil
		}
		if n, err := io.ReadFull(r, hdr[4:8]); err != nil {
			return append(buf, hdr[:4+n]...), 0, false, err
		}
		skip := int64(binary.LittleEndian.Uint32(hdr[4:8]))
		if _, err := io.CopyN(io.Discard, r, skip); err != nil {
			// The stream decoder reports the truncated frame.
			return append(buf, hdr[:8]...), 0, false, err
		}
	}

	// Frame header (RFC 8878 3.1.1.1) and the first block header.
	if _, err := io.ReadFull(r, hdr[4:5]); err != nil {
		return append(buf, hdr[:4]...), 0, false, err
	}
	// Dictionary ID and content size fields, then a window descriptor or a
	// one-byte content size.
	fhd := hdr[4]
	hdrLen := 5 + [4]int{0, 1, 2, 4}[fhd&3] + [4]int{0, 2, 4, 8}[fhd>>6]
	if fhd&(1<<5) == 0 || fhd>>6 == 0 {
		hdrLen++
	}
	if n, err := io.ReadFull(r, hdr[5:hdrLen+3]); err != nil {
		return append(buf, hdr[:5+n]...), 0, false, err
	}
	buf = append(buf, hdr[:hdrLen+3]...)
	var h Header
	if err := h.Decode(hdr[:hdrLen+3]); err != nil {
		return buf, 0, false, nil
	}
	size = fcsUnknown
	if h.HasFCS {
		size = h.FrameContentSize
	}
	window := h.WindowSize
	if h.SingleSegment {
		window = max(size, MinWindowSize)
	}
	if window > maxWindow || (h.HasFCS && size > limit) {
		return buf, 0, false, nil
	}

	// Blocks (RFC 8878 3.1.1.2). Each read takes a block and the header of
	// the next, or the checksum after the last block.
	var decoded uint64
	for {
		bh := buf[len(buf)-3:]
		v := uint32(bh[0]) | uint32(bh[1])<<8 | uint32(bh[2])<<16
		last := v&1 != 0
		n := int(v >> 3)
		switch blockType((v >> 1) & 3) {
		case blockTypeRLE:
			n = 1
		case blockTypeReserved:
			return buf, 0, false, nil
		}
		decoded += maxCompressedBlockSize
		if n > maxCompressedBlockSize || (!h.HasFCS && decoded > limit) ||
			uint64(len(buf)) > limit+limit/16+1024 {
			return buf, 0, false, nil
		}
		next := 3
		if last {
			next = 0
			if h.HasCheckSum {
				next = 4
			}
		}
		start := len(buf)
		buf = slices.Grow(buf, n+next)[:start+n+next]
		if m, err := io.ReadFull(r, buf[start:]); err != nil {
			return buf[:start+m], 0, false, err
		}
		if last {
			return buf, size, true, nil
		}
	}
}

// errorOr returns r, or a reader that returns err if err is not an EOF.
func errorOr(r io.Reader, err error) io.Reader {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return r
	}
	return errReader{err}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
