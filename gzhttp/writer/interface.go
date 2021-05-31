package writer

import "io"

// GzipWriter implements the functions needed for compressing content.
type GzipWriter interface {
	Close() error
	Flush() error
	Write(p []byte) (int, error)
}

// GzipWriterFactory contains the information needed for the
type GzipWriterFactory struct {
	// Must return the minimum and maximum supported level.
	Levels func() (min, max int)

	// New must return a new GzipWriter.
	// level will always be within the return limits above.
	New func(writer io.Writer, level int) GzipWriter
}
