//go:build unix

package main

import (
	"errors"
	"syscall"
)

func isIncompleteSymlinkResolution(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR)
}
