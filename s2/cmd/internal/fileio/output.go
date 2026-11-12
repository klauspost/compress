package fileio

import (
	"io/fs"
	"os"
)

// Create opens name for output. Exclusive creation fails if any directory
// entry, including a dangling symbolic link, already exists at name.
func Create(name string, mode fs.FileMode, exclusive bool) (*os.File, error) {
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if exclusive {
		flag |= os.O_EXCL
	}
	return os.OpenFile(name, flag, mode.Perm())
}
