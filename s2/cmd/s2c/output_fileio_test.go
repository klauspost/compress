package main

import (
	"io/fs"
	"os"

	"github.com/klauspost/compress/s2/cmd/internal/fileio"
)

type outputHandle interface {
	Chmod(os.FileMode) error
	Close() error
	Stat() (os.FileInfo, error)
	Sync() error
}

// These adapters keep the existing s2c lifecycle tests focused on behavior
// while their implementation is shared with s2d through fileio.
func createOutput(in fs.FileInfo, dst string, mode fs.FileMode, safe, remove bool) (*os.File, string, error) {
	return fileio.CreateOutput(in, dst, mode, safe, remove)
}

func replaceOutput(tmp, dst string) error {
	return fileio.ReplaceOutput(tmp, dst)
}

func finishOutput(f outputHandle, tmp, dst string, mode fs.FileMode, err error) error {
	return fileio.FinishOutput(f, tmp, dst, mode, err)
}

func openOutputFile(inputInfo fs.FileInfo, name string, mode fs.FileMode, exclusive bool) (*os.File, bool, error) {
	return fileio.OpenOutputFile(inputInfo, name, mode, exclusive)
}
