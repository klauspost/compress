package zstd

import (
	"errors"
	"fmt"
	"runtime"
)

// DOption is an for creating a decoder.
type DOption func(*decoderOptions) error

// options retains accumulated state of multiple options.
// See https://wiki.vivino.com/dev/server-side/api/elements/coupondefinition
type decoderOptions struct {
	lowMem         bool
	concurrent     int
	maxDecodedSize uint64
}

func (o *decoderOptions) setDefault() {
	*o = decoderOptions{
		// use less ram: true for now, but may change.
		lowMem:     true,
		concurrent: runtime.GOMAXPROCS(0),
	}
	if o.concurrent > 4 {
		o.concurrent = 4
	}
	o.maxDecodedSize = 1 << 63
}

// WithDecoderLowmem will set whether to use a lower amount of memory,
// but possibly have to allocate more while running.
func WithDecoderLowmem(b bool) DOption {
	return func(o *decoderOptions) error { o.lowMem = b; return nil }
}

// WithDecoderConcurrency will set the concurrency,
// meaning the maximum number of decoders to run concurrently.
// The value supplied must be at least 1.
// By default this will be set to GOMAXPROCS.
// In memory constrained systems, this
func WithDecoderConcurrency(n int) DOption {
	return func(o *decoderOptions) error {
		if n <= 0 {
			return fmt.Errorf("Concurrency must be at least 1")
		}
		o.concurrent = n
		return nil
	}
}

// WithDecoderMaxMemory allows to set a maximum decoded size for in-memory
// (non-streaming) operations.
// Maxmimum and default is 1 << 63 bytes.
func WithDecoderMaxMemory(n uint64) DOption {
	return func(o *decoderOptions) error {
		if n == 0 {
			return errors.New("WithDecoderMaxmemory must be at least 1")
		}
		if n > 1<<63 {
			return fmt.Errorf("WithDecoderMaxmemorymust be less than 1 << 63")
		}
		o.maxDecodedSize = n
		return nil
	}
}
