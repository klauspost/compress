// Package zstd provides decompression of zstandard files.
//
// For advanced usage and examples, go to the README: https://github.com/klauspost/compress/tree/master/zstd
package zstd

import (
	"log"
)

const debug = false

func println(a ...interface{}) {
	if debug {
		log.Println(a...)
	}
}

func printf(format string, a ...interface{}) {
	if debug {
		log.Printf(format, a...)
	}
}
