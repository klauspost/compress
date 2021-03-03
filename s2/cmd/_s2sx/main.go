package main

import (
	"bufio"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/s2"

	"github.com/klauspost/compress/s2/cmd/internal/readahead"
)

var (
	goos   = flag.String("os", runtime.GOOS, "Destination operating system")
	goarch = flag.String("arch", runtime.GOARCH, "Destination architecture")
	cpu    = flag.Int("cpu", runtime.GOMAXPROCS(0), "Compress using this amount of threads")
	safe   = flag.Bool("safe", false, "Do not overwrite output files")
	stdout = flag.Bool("c", false, "Write all output to stdout. Multiple input files will be concatenated")
	remove = flag.Bool("rm", false, "Delete source file(s) after successful compression")
	quiet  = flag.Bool("q", false, "Don't write any output to terminal, except errors")
	help   = flag.Bool("help", false, "Display help")

	version = "(dev)"
	date    = "(unknown)"
)

//go:embed sfx-exe
var embeddedFiles embed.FS

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 || *help {
		_, _ = fmt.Fprintf(os.Stderr, "s2 selfextraction v%v, built at %v.\n\n", version, date)
		_, _ = fmt.Fprintf(os.Stderr, "Copyright (c) 2011 The Snappy-Go Authors. All rights reserved.\n"+
			"Copyright (c) 2021 Klaus Post. All rights reserved.\n\n")
		_, _ = fmt.Fprintln(os.Stderr, `Usage: sfx [options] file1 file2

Compresses all files supplied as input separately.
Output files are written as 'filename.ext.s2'.
By default output files will be overwritten.
Use - as the only file name to read from stdin and write to stdout.

Wildcards are accepted: testdir/*.txt will compress all files in testdir ending with .txt
Directories can be wildcards as well. testdir/*/*.txt will match testdir/subdir/b.txt

Options:`)
		flag.PrintDefaults()
		os.Exit(1)
	}

	opts := []s2.WriterOption{s2.WriterBestCompression(), s2.WriterConcurrency(*cpu), s2.WriterBlockSize(4 << 20)}
	wr := s2.NewWriter(nil, opts...)
	var files []string
	for _, pattern := range args {
		found, err := filepath.Glob(pattern)
		exitErr(err)
		if len(found) == 0 {
			exitErr(fmt.Errorf("unable to find file %v", pattern))
		}
		files = append(files, found...)
	}
	wantPlat := *goos + "-" + *goarch
	exec, err := embeddedFiles.ReadFile(path.Join("sfx-exe", wantPlat))
	if os.IsNotExist(err) {
		dir, err := embeddedFiles.ReadDir("sfx-exe")
		exitErr(err)
		_, _ = fmt.Fprintf(os.Stderr, "os-arch %v not available. Available sfx platforms are:\n\n", wantPlat)
		for _, d := range dir {
			_, _ = fmt.Fprintf(os.Stderr, "* %s\n", d.Name())
		}
		_, _ = fmt.Fprintf(os.Stderr, "\nUse -os and -arch to specify the destination platform.")
		os.Exit(1)
	}
	//
	if *stdout {
		// Write exec once to stdout
		_, err = os.Stdout.Write(exec)
	}
	for _, filename := range files {
		func() {
			var closeOnce sync.Once
			isCompressed := strings.HasSuffix(filename, ".s2")
			filename = strings.TrimPrefix(filename, ".s2")
			dstFilename := fmt.Sprintf("%s%s", filename, ".s2sfx")
			if *goos == "windows" {
				dstFilename += ".exe"
			}
			if !*quiet {
				if !isCompressed {
					fmt.Print("Compressing ", filename, " -> ", dstFilename, " for ", wantPlat)
				} else {
					fmt.Print("Creating sfx archive ", filename, " -> ", dstFilename, " for ", wantPlat)
				}
			}
			// Input file.
			file, err := os.Open(filename)
			exitErr(err)
			defer closeOnce.Do(func() { file.Close() })
			src, err := readahead.NewReaderSize(file, *cpu+1, 1<<20)
			exitErr(err)
			defer src.Close()
			finfo, err := file.Stat()
			exitErr(err)
			var out io.Writer
			switch {
			case *stdout:
				out = os.Stdout
			default:
				mode := finfo.Mode() // use the same mode for the output file
				if *safe {
					_, err := os.Stat(dstFilename)
					if !os.IsNotExist(err) {
						exitErr(errors.New("destination file exists"))
					}
				}
				dstFile, err := os.OpenFile(dstFilename, os.O_CREATE|os.O_WRONLY, mode)
				exitErr(err)
				defer dstFile.Close()
				bw := bufio.NewWriterSize(dstFile, 4<<20*2)
				defer bw.Flush()
				out = bw
				_, err = out.Write(exec)
			}
			exitErr(err)
			wc := wCounter{out: out}
			start := time.Now()
			var input int64
			if !isCompressed {
				wr.Reset(&wc)
				defer wr.Close()
				input, err = wr.ReadFrom(src)
				exitErr(err)
				err = wr.Close()
				exitErr(err)
			} else {
				input, err = io.Copy(&wc, src)
				exitErr(err)
			}
			if !*quiet {
				elapsed := time.Since(start)
				mbpersec := (float64(input) / (1024 * 1024)) / (float64(elapsed) / (float64(time.Second)))
				pct := float64(wc.n) * 100 / float64(input)
				fmt.Printf(" %d -> %d [%.02f%%]; %.01fMB/s\n", input, wc.n, pct, mbpersec)
			}
			if *remove {
				closeOnce.Do(func() {
					file.Close()
					if !*quiet {
						fmt.Println("Removing", filename)
					}
					err := os.Remove(filename)
					exitErr(err)
				})
			}
		}()
	}

}

func exitErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nERROR:", err.Error())
		os.Exit(2)
	}
}

type wCounter struct {
	n   int
	out io.Writer
}

func (w *wCounter) Write(p []byte) (n int, err error) {
	n, err = w.out.Write(p)
	w.n += n
	return n, err

}
