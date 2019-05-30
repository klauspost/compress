package zstd

import "runtime"

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
