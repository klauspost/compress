package main

import (
	"archive/tar"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/s2"
)

const (
	opUnpack = iota + 1
	opUnTar
)

func main() {
	me, err := os.Executable()
	exitErr(err)
	f, err := os.Open(me)
	exitErr(err)
	defer f.Close()
	stat, err := f.Stat()
	exitErr(err)
	rd, err := newReader(f, stat.Size())
	exitErr(err)
	var tmp [1]byte
	_, err = io.ReadFull(rd, tmp[:])
	exitErr(err)
	dec := s2.NewReader(rd)
	switch tmp[0] {
	case opUnpack:
		outname := me + "-extracted"
		if idx := strings.Index(me, ".s2sfx"); idx > 0 {
			// Trim from '.s2sfx'
			outname = me[:idx]
		}
		fmt.Printf("Extracting to %q...", outname)
		out, err := os.Create(outname)
		exitErr(err)
		_, err = io.Copy(out, dec)
		exitErr(err)

	case opUnTar:
		dir, err := os.Getwd()
		if err != nil {
			dir = filepath.Dir(me)
		}
		fmt.Printf("Extracting TAR file to %s...\n", dir)
		exitErr(untar(dir, dec))
	default:
		exitErr(fmt.Errorf("unknown operation: %d", tmp[0]))
	}
	fmt.Println("\nDone.")
}

func exitErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nERROR:", err.Error())
		os.Exit(2)
	}
}

func newReader(rda io.ReaderAt, size int64) (io.Reader, error) {
	handlers := []func(io.ReaderAt, int64) (io.Reader, error){
		exeReaderMacho,
		exeReaderElf,
		exeReaderPe,
	}

	for _, handler := range handlers {
		zfile, err := handler(rda, size)
		if err == nil {
			return zfile, nil
		}
	}
	return nil, errors.New("no archive data found")
}

// zipExeReaderPe treats the file as a Portable Executable binary
func exeReaderPe(rda io.ReaderAt, size int64) (io.Reader, error) {
	file, err := pe.NewFile(rda)
	if err != nil {
		return nil, err
	}

	var max int64
	for _, sec := range file.Sections {
		end := int64(sec.Offset + sec.Size)
		if end > max {
			max = end
		}
	}

	if size == max {
		return nil, errors.New("data not found")
	}
	return io.NewSectionReader(rda, max, size-max), nil
}

// zipExeReaderElf treats the file as a ELF binary
func exeReaderElf(rda io.ReaderAt, size int64) (io.Reader, error) {
	file, err := elf.NewFile(rda)
	if err != nil {
		return nil, err
	}

	var max int64
	for _, sect := range file.Sections {
		if sect.Type == elf.SHT_NOBITS {
			continue
		}

		// Move to end of file pointer
		end := int64(sect.Offset + sect.Size)
		if end > max {
			max = end
		}
	}

	if size == max {
		return nil, errors.New("data not found")
	}
	return io.NewSectionReader(rda, max, size-max), nil
}

// zipExeReaderMacho treats the file as a Mach-O binary
func exeReaderMacho(rda io.ReaderAt, size int64) (io.Reader, error) {
	file, err := macho.NewFile(rda)
	if err != nil {
		return nil, err
	}

	var max int64
	for _, load := range file.Loads {
		seg, ok := load.(*macho.Segment)
		if ok {
			// Move to end of file pointer
			end := int64(seg.Offset + seg.Filesz)
			if end > max {
				max = end
			}
		}
	}

	// No zip file within binary, try appended to end
	if size == max {
		return nil, errors.New("data not found")
	}
	return io.NewSectionReader(rda, max, size-max), nil
}

// untar takes a destination path and a reader; a tar reader loops over the tarfile
// creating the file structure at 'dst' along the way, and writing any files
func untar(dst string, r io.Reader) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		if err := checkPath(dst, header.Name); err != nil {
			return err
		}
		target := filepath.Join(dst, header.Name)

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			fmt.Println(target)
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg, tar.TypeChar, tar.TypeBlock, tar.TypeFifo, tar.TypeGNUSparse:
			target = path.Clean(target)
			fmt.Println(target)

			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				if os.IsNotExist(err) {
					if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
						return err
					}
					f, err = os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
				}
				if err != nil {
					return err
				}
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			f.Close()
		case tar.TypeSymlink:
			target = path.Clean(target)
			fmt.Println(target)

			err := writeNewSymbolicLink(target, header.Linkname)
			if err != nil {
				return err
			}
		case tar.TypeLink:
			target = path.Clean(target)
			fmt.Println(target)

			err := writeNewHardLink(target, filepath.Join(dst, header.Linkname))
			if err != nil {
				return err
			}
		}
	}
}

// Thanks to https://github.com/mholt/archiver for the following:

func checkPath(dst, filename string) error {
	dest := filepath.Join(dst, filename)
	//prevent path traversal attacks
	if !strings.HasPrefix(dest, dst) {
		return fmt.Errorf("illegal file path: %s", filename)
	}
	return nil
}

func writeNewSymbolicLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %+v", fpath, err)
		}
	}

	err = os.Symlink(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making symbolic link for: %v", fpath, err)
	}
	return nil
}

func writeNewHardLink(fpath string, target string) error {
	err := os.MkdirAll(filepath.Dir(fpath), 0755)
	if err != nil {
		return fmt.Errorf("%s: making directory for file: %v", fpath, err)
	}

	_, err = os.Lstat(fpath)
	if err == nil {
		err = os.Remove(fpath)
		if err != nil {
			return fmt.Errorf("%s: failed to unlink: %+v", fpath, err)
		}
	}

	err = os.Link(target, fpath)
	if err != nil {
		return fmt.Errorf("%s: making hard link for: %v", fpath, err)
	}
	return nil
}
