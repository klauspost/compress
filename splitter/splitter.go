// Package splitter splits an input stream into fragments
package splitter

import (
	"errors"
	"fmt"
	"io"
)

type Option func(o *options) error

var With = Option(nil)

func (w Option) parent(o *options) error {
	if w != nil {
		return w(o)
	}
	return nil
}

func (w Option) MaxBlockSize(n uint) Option {
	return func(o *options) error {
		if err := w.parent(o); err != nil {
			return err
		}
		if n < MinBlockSize {
			return ErrSizeTooSmall
		}
		o.maxBlockSize = n
		return nil
	}
}

func (w Option) Mode(m Mode) Option {
	return func(o *options) error {
		if err := w.parent(o); err != nil {
			return err
		}
		o.mode = m
		return nil
	}
}

func (w Option) BackChannel(ch <-chan []byte) Option {
	return func(o *options) error {
		if err := w.parent(o); err != nil {
			return err
		}
		o.sendBack = ch
		return nil
	}
}

func (w Option) SendBack(size int) (Option, func([]byte)) {
	ch := make(chan []byte, size)
	return func(o *options) error {
			if err := w.parent(o); err != nil {
				return err
			}
			o.sendBack = ch
			return nil
		}, func(b []byte) {
			select {
			case ch <- b:
			default:
			}
		}
}

func defaultOptions() options {
	return options{
		maxBlockSize: 256 << 10,
		minBlockSize: 64 << 10,
		mode:         ModeEntropy,
	}
}

type options struct {
	minBlockSize uint
	maxBlockSize uint
	sendBack     <-chan []byte
	mode         Mode
}

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
	opts    options
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
func New(fragments chan<- []byte, opts ...Option) (io.WriteCloser, error) {
	o := defaultOptions()
	for _, opt := range opts {
		err := opt(&o)
		if err != nil {
			return nil, err
		}
	}
	// For small block sizes we need to keep a pretty big buffer to keep input fed.
	// Constant below appears to be sweet spot measured with 4K blocks.
	var bufmul = 256 << 10 / int(o.maxBlockSize)
	if bufmul < 2 {
		bufmul = 2
	}

	w := &writer{
		frags:   fragments,
		maxSize: int(o.maxBlockSize),
		opts:    o,
	}

	switch o.mode {
	case ModeFixed:
		fw := newFixedWriter(o.maxBlockSize, fragments)
		fw.sb = w.opts.sendBack
		w.writer = fw.write
		w.split = fw.split
	case ModePrediction:
		zw := newPredictionWriter(o.maxBlockSize, fragments)
		zw.sb = w.opts.sendBack
		w.writer = zw.write
		w.split = zw.split
	case ModeEntropy:
		zw := newEntropyWriter(o.maxBlockSize, fragments)
		zw.sb = w.opts.sendBack
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

type sendBack <-chan []byte

func (s sendBack) getBuffer(size int) []byte {
	select {
	case b := <-s:
		if cap(b) >= size {
			return b[:size]
		}
	default:
	}
	return make([]byte, size)
}
