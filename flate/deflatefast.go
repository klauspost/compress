package flate

import (
	"fmt"
	"io"
)

type deflateFast struct {
	w    *huffmanBitWriter
	fill func([]byte) int // copy data to window
	step func()           // process window
	sync bool             // requesting flush

	window    [maxStoreBlockSize]byte
	windowEnd int
	// output tokens
	tokens tokens
	snap   snappyEnc
	err    error
}

func (d *deflateFast) init(w io.Writer, level int) (err error) {
	d.w = newHuffmanBitWriter(w)
	switch {
	case level == NoCompression:
		d.fill = d.fillBlock
		d.step = d.store
	case level == ConstantCompression:
		d.fill = d.fillBlock
		d.step = d.storeHuff
	case level >= 1 && level <= 4:
		d.snap = newSnappy(level)
		d.fill = d.fillBlock
		d.step = d.storeSnappy
	default:
		return fmt.Errorf("deflateFast: invalid compression level %d: want value in range [-2,0,1,2,3,4]", level)

	}
	return nil
}

func (d *deflateFast) store() {
	if d.windowEnd > 0 && (d.windowEnd == maxStoreBlockSize || d.sync) {
		d.err = d.writeStoredBlock(d.window[:d.windowEnd])
		d.windowEnd = 0
	}
}

// storeHuff will compress and store the currently added data,
// if enough has been accumulated or we at the end of the stream.
// Any error that occurred will be in d.err
func (d *deflateFast) storeHuff() {
	if d.windowEnd < len(d.window) && !d.sync || d.windowEnd == 0 {
		return
	}
	d.w.writeBlockHuff(false, d.window[:d.windowEnd])
	d.err = d.w.err
	d.windowEnd = 0
}

// storeHuff will compress and store the currently added data,
// if enough has been accumulated or we at the end of the stream.
// Any error that occurred will be in d.err
func (d *deflateFast) storeSnappy() {
	// We only compress if we have maxStoreBlockSize.
	if d.windowEnd < maxStoreBlockSize {
		if !d.sync {
			return
		}
		// Handle extremely small sizes.
		if d.windowEnd < 128 {
			if d.windowEnd == 0 {
				return
			}
			if d.windowEnd <= 32 {
				d.err = d.writeStoredBlock(d.window[:d.windowEnd])
				d.windowEnd = 0
			} else {
				d.w.writeBlockHuff(false, d.window[:d.windowEnd])
				d.err = d.w.err
			}
			d.windowEnd = 0
			d.snap.Reset()
			return
		}
	}

	d.snap.Encode(&d.tokens, d.window[:d.windowEnd])
	// If we made zero matches, store the block as is.
	if int(d.tokens.n) == d.windowEnd {
		d.err = d.writeStoredBlock(d.window[:d.windowEnd])
		// If we removed less than 1/16th, huffman compress the block.
	} else if int(d.tokens.n) > d.windowEnd-(d.windowEnd>>4) {
		d.w.writeBlockHuff(false, d.window[:d.windowEnd])
		d.err = d.w.err
	} else {
		d.w.writeBlockDynamic(&d.tokens, false, d.window[:d.windowEnd])
		d.err = d.w.err
	}
	d.tokens.Reset()
	d.windowEnd = 0
}

func (d *deflateFast) writeStoredBlock(buf []byte) error {
	if d.w.writeStoredHeader(len(buf), false); d.w.err != nil {
		return d.w.err
	}
	d.w.writeBytes(buf)
	return d.w.err
}

// reset the state of the compressor.
func (d *deflateFast) reset(w io.Writer) {
	d.w.reset(w)
	d.sync = false
	d.err = nil
	d.windowEnd = 0
	// We only need to reset a few things for Snappy.
	if d.snap != nil {
		d.snap.Reset()
		d.tokens.Reset()
	}
}

// fillWindow will fill the buffer with data for huffman-only compression.
// The number of bytes copied is returned.
func (d *deflateFast) fillBlock(b []byte) int {
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

// write will add input byte to the stream.
// Unless an error occurs all bytes will be consumed.
func (d *deflateFast) write(b []byte) (n int, err error) {
	if d.err != nil {
		return 0, d.err
	}
	n = len(b)
	for len(b) > 0 {
		d.step()
		b = b[d.fill(b):]
		if d.err != nil {
			return 0, d.err
		}
	}
	return n, d.err
}

func (d *deflateFast) syncFlush() error {
	d.sync = true
	if d.err != nil {
		return d.err
	}
	d.step()
	if d.err == nil {
		d.w.writeStoredHeader(0, false)
		d.w.flush()
		d.err = d.w.err
	}
	d.tokens.Reset()
	d.sync = false
	return d.err
}

func (d *deflateFast) close() error {
	if d.err != nil {
		return d.err
	}
	d.sync = true
	d.step()
	if d.err != nil {
		return d.err
	}
	if d.w.writeStoredHeader(0, true); d.w.err != nil {
		return d.w.err
	}
	d.w.flush()
	return d.w.err
}
