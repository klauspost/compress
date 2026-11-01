package main

import (
	"archive/tar"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/s2"
)

const (
	opUnpack = iota + 1
	opUnTar
)

var (
	help       = flag.Bool("help", false, "Display help")
	untarFlag  = flag.Bool("untar", true, "Untar tar files if specified at creation")
	forceUntar = flag.Bool("force-untar", false, "Always untar file")
	quiet      = flag.Bool("q", false, "Don't write any output to terminal, except errors")
)

func main() {
	flag.Parse()
	me, err := os.Executable()
	exitErr(err)
	args := flag.Args()
	invalidArgs := len(args) > 1
	if *help || invalidArgs {
		_, _ = fmt.Fprintf(os.Stderr, "s2sx Self Extracting Archive\n\n")
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s [options] [output-file/dir]\n\n", os.Args[0])
		_, _ = fmt.Fprintf(os.Stderr, "Use - as file name to extract to stdout when not untarring.\n\n")
		_, _ = fmt.Fprintln(os.Stderr, `Options:`)
		flag.PrintDefaults()
		if invalidArgs {
			os.Exit(1)
		}
		os.Exit(0)
	}
	stdout := len(args) > 0 && args[0] == "-"
	*quiet = *quiet || stdout

	f, err := os.Open(me)
	exitErr(err)
	defer f.Close()
	stat, err := f.Stat()
	exitErr(err)
	rd, err := newReader(f, stat.Size())
	exitErr(err)
	f2, err := os.Open(me + ".more")
	if err == nil {
		rd = io.MultiReader(rd, f2)
	}
	if !os.IsNotExist(err) {
		exitErr(err)
	}
	var tmp [1]byte
	_, err = io.ReadFull(rd, tmp[:])
	exitErr(err)
	dec := s2.NewReader(rd)
	if !*untarFlag {
		tmp[0] = opUnpack
	}
	if *forceUntar {
		tmp[0] = opUnTar
	}
	switch tmp[0] {
	case opUnpack:
		outname := me + "-extracted"
		if idx := strings.Index(me, ".s2sx"); idx > 0 {
			// Trim from '.s2sx'
			outname = me[:idx]
		}
		var out io.Writer
		if stdout {
			out = os.Stdout
		} else {
			if len(args) > 0 {
				outname, err = filepath.Abs(args[0])
				exitErr(err)
			}
			if !*quiet {
				fmt.Printf("Extracting to \"%s\"...", outname)
			}
			f, err := os.Create(outname)
			exitErr(err)
			defer f.Close()
			out = f
		}
		_, err = dec.DecodeConcurrent(out, 0)
		exitErr(err)

	case opUnTar:
		dir, err := os.Getwd()
		if err != nil {
			dir = filepath.Dir(me)
		}
		if len(args) > 0 {
			if args[0] == "-" {
				exitErr(errors.New("cannot untar files to stdout. Use -untar=false to skip untar operation"))
			}
			if filepath.IsAbs(args[0]) {
				dir = args[0]
			} else {
				dir = filepath.Join(dir, args[0])
			}
		}
		if !*quiet {
			fmt.Printf("Extracting TAR file to %s...\n", dir)
		}
		exitErr(untar(dir, dec))
	default:
		exitErr(fmt.Errorf("unknown operation: %d", tmp[0]))
	}
	if !*quiet {
		fmt.Println("\nDone.")
	}
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
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(dst)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("extraction root cannot be a symbolic link: %s", dst)
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(info, rootInfo) {
		return fmt.Errorf("extraction root changed while opening: %s", dst)
	}

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

		target, err := cleanArchivePath(header.Name)
		if err != nil {
			return err
		}
		displayTarget := filepath.Join(dst, target)

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if !*quiet {
				fmt.Println(displayTarget)
			}
			if err := root.MkdirAll(target, 0o755); err != nil {
				return err
			}

		// if it's a file create it
		case tar.TypeReg, tar.TypeChar, tar.TypeBlock, tar.TypeFifo, tar.TypeGNUSparse:
			if !*quiet {
				fmt.Println(displayTarget)
			}
			if err := root.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := root.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			if err := errors.Join(copyErr, f.Close()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := checkLinkTarget(target, header.Linkname); err != nil {
				return err
			}
			if !*quiet {
				fmt.Println(displayTarget)
			}
			if err := writeNewSymbolicLink(root, target, filepath.FromSlash(header.Linkname)); err != nil {
				return err
			}
		case tar.TypeLink:
			linkTarget, err := cleanArchivePath(header.Linkname)
			if err != nil {
				return err
			}
			if !*quiet {
				fmt.Println(displayTarget)
			}
			if err := writeNewHardLink(root, target, linkTarget); err != nil {
				return err
			}
		}
	}
}

func cleanArchivePath(name string) (string, error) {
	name = filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" ||
		strings.HasPrefix(name, string(os.PathSeparator)) ||
		name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal file path: %s", name)
	}
	return name, nil
}

func checkLinkTarget(name, linkTarget string) error {
	linkTarget = filepath.FromSlash(linkTarget)
	if filepath.IsAbs(linkTarget) || filepath.VolumeName(linkTarget) != "" ||
		strings.HasPrefix(linkTarget, string(os.PathSeparator)) {
		return fmt.Errorf("illegal link target: %s", linkTarget)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(name), linkTarget))
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("illegal link target: %s", linkTarget)
	}
	return nil
}

func writeNewSymbolicLink(root *os.Root, name, target string) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("%s: making directory for file: %w", name, err)
	}
	if err := removeExisting(root, name); err != nil {
		return fmt.Errorf("%s: failed to unlink: %w", name, err)
	}
	if err := root.Symlink(target, name); err != nil {
		return fmt.Errorf("%s: making symbolic link: %w", name, err)
	}
	if _, err := root.Stat(name); err != nil {
		_ = root.Remove(name)
		return fmt.Errorf("%s: illegal link target: %w", name, err)
	}
	return nil
}

func writeNewHardLink(root *os.Root, name, target string) error {
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("%s: making directory for file: %w", name, err)
	}
	if err := removeExisting(root, name); err != nil {
		return fmt.Errorf("%s: failed to unlink: %w", name, err)
	}
	if err := root.Link(target, name); err != nil {
		return fmt.Errorf("%s: making hard link: %w", name, err)
	}
	return nil
}

func removeExisting(root *os.Root, name string) error {
	_, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return root.Remove(name)
}
