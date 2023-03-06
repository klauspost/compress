package writer

import (
	"io"
	"time"
)

// GzipWriter implements the functions needed for compressing content.
type GzipWriter interface {
	Write(p []byte) (int, error)
	Close() error
	Flush() error
}

// GzipWriterExt implements the functions needed for compressing content
// and optional extensions.
type GzipWriterExt interface {
	GzipWriter

	// SetHeader will populate header fields with non-nil values in h.
	SetHeader(h Header)
}

// Header provides nillable header fields.
type Header struct {
	Comment []byte     // comment, converted to string if set.
	Extra   *[]byte    // "extra data"
	ModTime *time.Time // modification time
	Name    []byte     // file name, converted to string if set.
	OS      *byte      // operating system type
}

// GzipWriterFactory contains the information needed for custom gzip implementations.
type GzipWriterFactory struct {
	// Must return the minimum and maximum supported level.
	Levels func() (min, max int)

	// New must return a new GzipWriter.
	// level will always be within the return limits above.
	New func(writer io.Writer, level int) GzipWriter
}
