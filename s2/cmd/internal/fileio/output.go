package fileio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Output is a destination file that must be committed after all writes finish.
type Output struct {
	file      *os.File
	finalName string
	tempName  string
	finalMode fs.FileMode
	restore   bool
}

// Create opens name for output. Exclusive creation fails if any directory
// entry, including a dangling symbolic link, already exists at name.
func Create(name string, mode fs.FileMode, exclusive bool) (*Output, error) {
	file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err == nil {
		info, statErr := file.Stat()
		if statErr != nil {
			return nil, cleanupCreated(file, name, statErr)
		}
		if chmodErr := file.Chmod(0o600); chmodErr != nil {
			return nil, cleanupCreated(file, name, chmodErr)
		}
		return &Output{
			file:      file,
			finalMode: info.Mode().Perm(),
			restore:   true,
		}, nil
	}
	if exclusive || !errors.Is(err, fs.ErrExist) {
		if exclusive && errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("destination file exists: %w", err)
		}
		return nil, err
	}

	file, err = os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return nil, err
	}
	return &Output{file: file}, nil
}

// CreateTemp creates a private temporary file next to finalName. Commit
// atomically renames it over finalName after all data and metadata are synced.
func CreateTemp(finalName string, mode fs.FileMode) (*Output, error) {
	dir := filepath.Dir(finalName)
	pattern := "." + filepath.Base(finalName) + ".tmp-*"
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &Output{
		file:      file,
		finalName: finalName,
		tempName:  file.Name(),
		finalMode: mode.Perm(),
		restore:   true,
	}, nil
}

// Name returns the file currently receiving output.
func (o *Output) Name() string {
	if o == nil || o.file == nil {
		return ""
	}
	return o.file.Name()
}

// Write writes to the output file.
func (o *Output) Write(p []byte) (int, error) {
	if o == nil || o.file == nil {
		return 0, fs.ErrClosed
	}
	return o.file.Write(p)
}

// Commit syncs and closes the output. Temporary output is assigned its final
// permission bits and atomically renamed only after the close succeeds.
func (o *Output) Commit() error {
	if o == nil || o.file == nil {
		return fs.ErrClosed
	}
	if err := o.file.Sync(); err != nil {
		return o.failCommit(err)
	}
	if o.restore {
		if err := o.file.Chmod(o.finalMode); err != nil {
			return o.failCommit(err)
		}
		if err := o.file.Sync(); err != nil {
			return o.failCommit(err)
		}
	}

	closeErr := o.file.Close()
	o.file = nil
	if closeErr != nil {
		return errors.Join(closeErr, o.removeTemp())
	}
	if o.tempName == "" {
		return nil
	}

	if err := os.Rename(o.tempName, o.finalName); err != nil {
		return errors.Join(err, o.removeTemp())
	}
	o.tempName = ""
	return nil
}

func (o *Output) failCommit(cause error) error {
	closeErr := o.file.Close()
	o.file = nil
	return errors.Join(cause, closeErr, o.removeTemp())
}

func cleanupCreated(file *os.File, name string, cause error) error {
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return errors.Join(cause, closeErr, removeErr)
}

// Abort closes an uncommitted output and removes any temporary file.
func (o *Output) Abort() error {
	if o == nil {
		return nil
	}
	var closeErr error
	if o.file != nil {
		closeErr = o.file.Close()
		o.file = nil
	}
	return errors.Join(closeErr, o.removeTemp())
}

func (o *Output) removeTemp() error {
	if o.tempName == "" {
		return nil
	}
	err := os.Remove(o.tempName)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	o.tempName = ""
	return err
}
