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
	single     bool
}

func (o *encoderOptions) setDefault() {
	*o = encoderOptions{
		// use less ram: true for now, but may change.
		concurrent: runtime.GOMAXPROCS(0),
		crc:        true,
		single:     false,
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

// WithSingleSegment will set the "single segment" flag when EncodeAll is used.
// If this flag is set, data must be regenerated within a single continuous memory segment.
// In this case, Window_Descriptor byte is skipped, but Frame_Content_Size is necessarily present.
// As a consequence, the decoder must allocate a memory segment of size equal or larger than size of your content.
// In order to preserve the decoder from unreasonable memory requirements,
// a decoder is allowed to reject a compressed frame which requests a memory size beyond decoder's authorized range.
// For broader compatibility, decoders are recommended to support memory sizes of at least 8 MB.
// This is only a recommendation, each decoder is free to support higher or lower limits, depending on local limitations.
// This setting has no effect on streamed encodes.
func WithSingleSegment(b bool) EOption {
	return func(o *encoderOptions) error {
		o.single = b
		return nil
	}
}
