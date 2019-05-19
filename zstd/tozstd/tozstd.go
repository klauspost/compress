package tozstd

import "log"

// Enable debug printing and extra checks.
const debug = false

// force encoder to use predefined tables.
const forcePreDef = false

// zstdMinMatch is the minimum zstd match length.
const zstdMinMatch = 3

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
