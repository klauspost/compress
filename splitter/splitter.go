// Package splitter splits an input stream into fragments
package splitter

import (
	"errors"
	"fmt"
	"io"
)

// Mode used to determine how input is split.
type Mode int

// The smallest "maximum" block size allowed.
const MinBlockSize = 512

const (
	// Fixed block size
	//
	// This is by far the fastest mode, and checks for duplicates
	// In fixed block sizes.
	// It can be helpful to use the "Split" function to reset offset, which
	// will reset duplication search at the position you are at.
	ModeFixed Mode = 0

	// Dynamic block size.
	//
	// This mode will create a deduplicator that will split the contents written
	// to it into dynamically sized blocks.
	// The size given indicates the maximum block size. Average size is usually maxSize/4.
	// Minimum block size is maxSize/64.
	ModePrediction = 1

	// Dynamic block size.
	//
	// This mode will create a deduplicator that will split the contents written
	// to it into dynamically sized blocks.
	// The size given indicates the maximum block size. Average size is usually maxSize/4.
	// Minimum block size is maxSize/64.
	ModeEntropy = 2
)

// ErrSizeTooSmall is returned if the requested block size is smaller than
// hash size.
var ErrSizeTooSmall = errors.New("maximum block size too small. must be at least 512 bytes")

type writer struct {
	frags   chan<- []byte             // Fragment output
	maxSize int                       // Maximum block size
	writer  func([]byte) (int, error) // Writes are forwarded here.
	split   func()                    // Called when Split is called.
}

// New will return a writer you can write data to,
// and the file will be split into separate fragments.
//
// You must supply a fragment channel, that will output fragments for
// the data you have written. The channel must accept data while you
// write to the spliter.
//
// When you call Close on the returned Writer, the final fragments
// will be sent and the channel will be closed.
func New(fragments chan<- []byte, mode Mode, maxSize uint) (io.WriteCloser, error) {
	// For small block sizes we need to keep a pretty big buffer to keep input fed.
	// Constant below appears to be sweet spot measured with 4K blocks.
	var bufmul = 256 << 10 / int(maxSize)
	if bufmul < 2 {
		bufmul = 2
	}

	w := &writer{
		frags:   fragments,
		maxSize: int(maxSize),
	}

	switch mode {
	case ModeFixed:
		fw := newFixedWriter(maxSize, fragments)
		w.writer = fw.write
		w.split = fw.split
	case ModePrediction:
		zw := newPredictionWriter(maxSize, fragments)
		w.writer = zw.write
		w.split = zw.split
	case ModeEntropy:
		zw := newEntropyWriter(maxSize, fragments)
		w.writer = zw.write
		w.split = zw.split
	default:
		return nil, fmt.Errorf("dedup: unknown mode")
	}

	if w.maxSize < MinBlockSize {
		return nil, ErrSizeTooSmall
	}

	return w, nil
}

// Write contents to the deduplicator.
func (w *writer) Write(b []byte) (n int, err error) {
	return w.writer(b)
}

// Close and flush the remaining data to output.
func (w *writer) Close() (err error) {
	if w.frags == nil {
		return nil
	}
	w.split()
	close(w.frags)
	w.frags = nil
	return nil
}
