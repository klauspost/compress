package zstd

import (
	"fmt"
	"runtime"
)

// DOption is an option for creating a encoder.
type EOption func(*encoderOptions) error

// options retains accumulated state of multiple options.
type encoderOptions struct {
	concurrent int
	crc        bool
}

func (o *encoderOptions) setDefault() {
	*o = encoderOptions{
		// use less ram: true for now, but may change.
		concurrent: runtime.GOMAXPROCS(0),
		crc:        true,
	}
}

// WithEncoderCRC will add CRC value to output.
// Output will be 4 bytes larger.
func WithEncoderCRC(b bool) EOption {
	return func(o *encoderOptions) error { o.crc = b; return nil }
}

// WithEncoderConcurrency will set the concurrency,
// meaning the maximum number of decoders to run concurrently.
// The value supplied must be at least 1.
// By default this will be set to GOMAXPROCS.
func WithEncoderConcurrency(n int) EOption {
	return func(o *encoderOptions) error {
		if n <= 0 {
			return fmt.Errorf("concurrency must be at least 1")
		}
		o.concurrent = n
		return nil
	}
}
