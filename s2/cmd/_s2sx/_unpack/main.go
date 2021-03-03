package main

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/s2"
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
	dec := s2.NewReader(rd)
	outname := me + "-extracted"
	if idx := strings.Index(me, ".s2sfx"); idx > 0 {
		// Trim from '.s2sfx'
		outname = me[:idx]
	}
	if strings.HasSuffix(outname, ".tar") {

	}
	fmt.Printf("Extracting to %q...", outname)
	out, err := os.Create(outname)
	exitErr(err)
	_, err = io.Copy(out, dec)
	exitErr(err)
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
