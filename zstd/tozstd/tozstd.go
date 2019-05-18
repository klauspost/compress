package tozstd

import "log"

// Enable debug printing and extra checks.
const debug = false

// force encoder to use predefined tables.
const forcePreDef = false

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
