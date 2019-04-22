package zstd

import (
	"github.com/klauspost/compress/huff0"
)

type history struct {
	b             []byte
	huffTree      *huff0.Scratch
	recentOffsets [3]int
	decoders      sequenceDecs
	windowSize    int
	maxSize       int
}

// reset will reset the history to initial state of a frame.
// The history must already have been initialized to the desired size.
func (h *history) reset() {
	h.b = h.b[:0]
	h.recentOffsets = [3]int{1, 4, 8}
	if f := h.decoders.litLengths.fse; f != nil && !f.preDefined {
		fseDecoderPool.Put(f)
	}
	if f := h.decoders.offsets.fse; f != nil && !f.preDefined {
		fseDecoderPool.Put(f)
	}
	if f := h.decoders.matchLengths.fse; f != nil && !f.preDefined {
		fseDecoderPool.Put(f)
	}
	h.decoders = sequenceDecs{}
	if h.huffTree != nil {
		huffDecoderPool.Put(h.huffTree)
	}
	h.huffTree = nil
	//printf("history created: %+v (l: %d, c: %d)", *h, len(h.b), cap(h.b))
}

// append bytes to history.
// This function will make sure there is space for it,
// if the buffer has been allocated with enough extra space.
func (h *history) append(b []byte) {
	if len(b) >= h.windowSize {
		// Discard all history by simply overwriting
		h.b = h.b[:h.windowSize]
		copy(h.b, b[len(b)-h.windowSize:])
		return
	}

	// If there is space, append it.
	if len(b) < cap(h.b)-len(h.b) {
		h.b = append(h.b, b...)
		return
	}

	// Move data down so we only have window size left.
	// We know we have less than window size input at this point.
	discard := len(b) + len(h.b) - h.windowSize
	copy(h.b, h.b[discard:])
	h.b = h.b[:h.windowSize]
	copy(h.b[h.windowSize-len(b):], b)
}

// append bytes to history without ever discarding anything.
func (h *history) appendKeep(b []byte) {
	h.b = append(h.b, b...)
}
