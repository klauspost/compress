package fileio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// CreateOutput opens dst. When dst aliases the input file it creates a
// temporary file next to dst instead, which FinishOutput renames over dst.
func CreateOutput(in fs.FileInfo, dst string, mode fs.FileMode, exclusive, remove bool) (*os.File, string, error) {
	if in != nil {
		outInfo, err := os.Stat(dst)
		if err == nil && os.SameFile(in, outInfo) {
			if !in.Mode().IsRegular() || !outInfo.Mode().IsRegular() {
				return nil, "", errors.New("input and output refer to the same non-regular file")
			}
			if remove {
				return nil, "", errors.New("cannot remove when input = output")
			}
			if exclusive {
				return nil, "", errors.New("cannot use -safe when output path is the same as the input file")
			}
			return createStagedOutput(dst)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
	}
	f, alias, err := OpenOutputFile(in, dst, mode, exclusive)
	if err != nil || !alias {
		return f, "", err
	}
	if remove {
		if f != nil {
			err = errors.Join(err, f.Close())
		}
		return nil, "", errors.Join(errors.New("cannot remove when input = output"), err)
	}
	if f != nil {
		err = f.Close()
		if err != nil {
			return nil, "", err
		}
	}
	return createStagedOutput(dst)
}

// ReplaceOutput renames tmp over dst. On systems that cannot replace an
// existing regular file directly, it keeps dst under a backup name until the
// replacement succeeds.
func ReplaceOutput(tmp, dst string) error {
	replaceErr := os.Rename(tmp, dst)
	if replaceErr == nil {
		return nil
	}
	tmpInfo, tmpStatErr := os.Lstat(tmp)
	if tmpStatErr != nil || !tmpInfo.Mode().IsRegular() {
		return replaceErr
	}
	dstInfo, dstStatErr := os.Lstat(dst)
	if dstStatErr != nil || !dstInfo.Mode().IsRegular() {
		return replaceErr
	}

	backup, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".backup-*")
	if err != nil {
		return errors.Join(replaceErr, err)
	}
	backupName := backup.Name()
	if err := errors.Join(backup.Close(), os.Remove(backupName)); err != nil {
		return errors.Join(replaceErr, err)
	}
	if err := os.Rename(dst, backupName); err != nil {
		return errors.Join(replaceErr, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		if restoreErr := os.Rename(backupName, dst); restoreErr != nil {
			return fmt.Errorf("replace %s: %w; original retained at %s", dst, errors.Join(replaceErr, err, restoreErr), backupName)
		}
		return errors.Join(replaceErr, err)
	}
	if err := os.Remove(backupName); err != nil {
		return fmt.Errorf("remove replacement backup %s: %w", backupName, err)
	}
	return nil
}

func createStagedOutput(dst string) (*os.File, string, error) {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return nil, "", err
	}
	return f, f.Name(), nil
}

type outputHandle interface {
	Chmod(os.FileMode) error
	Close() error
	Stat() (os.FileInfo, error)
	Sync() error
}

// FinishOutput closes f after writing ended with err. A temporary file
// replaces dst only if everything succeeded, and is removed otherwise.
func FinishOutput(f outputHandle, tmp, dst string, mode fs.FileMode, err error) error {
	if f == nil {
		return err
	}
	var removePath string
	var removeIdentity os.FileInfo
	if tmp == "" && dst != "" {
		if st, statErr := f.Stat(); statErr == nil && st.Mode().IsRegular() {
			removePath = dst
			removeIdentity = st
		}
	}
	if tmp != "" && err == nil {
		err = errors.Join(f.Sync(), f.Chmod(mode.Perm()))
	}
	closeErr := f.Close()
	err = errors.Join(err, closeErr)
	if tmp == "" {
		if closeErr != nil && removePath != "" && removeIdentity != nil {
			if post, statErr := os.Stat(removePath); statErr == nil && os.SameFile(removeIdentity, post) {
				err = errors.Join(err, os.Remove(removePath))
			}
		}
		return err
	}
	if err == nil {
		err = ReplaceOutput(tmp, dst)
	}
	if err != nil {
		removeErr := os.Remove(tmp)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		err = errors.Join(err, removeErr)
	}
	return err
}

// OpenOutputFile opens name without truncation, reports whether it aliases
// inputInfo, and truncates a distinct regular output only after that check.
func OpenOutputFile(inputInfo fs.FileInfo, name string, mode fs.FileMode, exclusive bool) (*os.File, bool, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if exclusive {
		flags |= os.O_EXCL
	}
	output, err := os.OpenFile(name, flags, mode.Perm())
	if err != nil {
		return nil, false, err
	}
	outputInfo, err := output.Stat()
	if err != nil {
		return nil, false, errors.Join(err, output.Close())
	}
	if inputInfo != nil && os.SameFile(inputInfo, outputInfo) {
		closeErr := output.Close()
		if !inputInfo.Mode().IsRegular() || !outputInfo.Mode().IsRegular() {
			return nil, false, errors.Join(errors.New("input and output refer to the same non-regular file"), closeErr)
		}
		return nil, true, closeErr
	}
	if outputInfo.Mode().IsRegular() {
		if err := output.Truncate(0); err != nil {
			return nil, false, errors.Join(err, output.Close())
		}
	}
	return output, false, nil
}
