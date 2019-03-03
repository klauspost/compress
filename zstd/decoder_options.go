package zstd

import (
	"fmt"
	"runtime"
)

// DOption is an for creating a decoder.
type DOption func(*decoderOptions) error

// options retains accumulated state of multiple options.
// See https://wiki.vivino.com/dev/server-side/api/elements/coupondefinition
type decoderOptions struct {
	lowMem     bool
	concurrent int
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
}

// WithLowmemDecoder will set whether to use a lower amount of memory,
// but possibly have to allocate more while running.
func WithLowmemDecoder(b bool) DOption {
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
