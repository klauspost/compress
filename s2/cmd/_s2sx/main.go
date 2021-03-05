package main

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
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

const (
	opUnpack = iota + 1
	opUnTar
)

var (
	goos   = flag.String("os", runtime.GOOS, "Destination operating system")
	goarch = flag.String("arch", runtime.GOARCH, "Destination architecture")
	cpu    = flag.Int("cpu", runtime.GOMAXPROCS(0), "Compress using this amount of threads")
	safe   = flag.Bool("safe", false, "Do not overwrite output files")
	stdout = flag.Bool("c", false, "Write all output to stdout. Multiple input files will be concatenated")
	remove = flag.Bool("rm", false, "Delete source file(s) after successful compression")
	quiet  = flag.Bool("q", false, "Don't write any output to terminal, except errors")
	untar  = flag.Bool("untar", false, "Untar on destination")
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
		_, _ = fmt.Fprintf(os.Stderr, "s2sx v%v, built at %v.\n\n", version, date)
		_, _ = fmt.Fprintf(os.Stderr, "Copyright (c) 2011 The Snappy-Go Authors. All rights reserved.\n"+
			"Copyright (c) 2021 Klaus Post. All rights reserved.\n\n")
		_, _ = fmt.Fprintln(os.Stderr, `Usage: s2sx [options] file1 file2

Compresses all files supplied as input separately.
If files have '.s2' extension they are assumed to be compressed already.
Output files are written as 'filename.s2sfx' and with '.exe' for windows targets.
By default output files will be overwritten.

Wildcards are accepted: testdir/*.txt will compress all files in testdir ending with .txt
Directories can be wildcards as well. testdir/*/*.txt will match testdir/subdir/b.txt

Options:`)
		flag.PrintDefaults()
		dir, err := embeddedFiles.ReadDir("sfx-exe")
		exitErr(err)
		_, _ = fmt.Fprintf(os.Stderr, "\nAvailable platforms are:\n\n")
		for _, d := range dir {
			_, _ = fmt.Fprintf(os.Stderr, " * %s\n", strings.TrimSuffix(d.Name(), ".s2"))
		}

		os.Exit(0)
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
	exec, err := embeddedFiles.ReadFile(path.Join("sfx-exe", wantPlat+".s2"))
	if os.IsNotExist(err) {
		dir, err := embeddedFiles.ReadDir("sfx-exe")
		exitErr(err)
		_, _ = fmt.Fprintf(os.Stderr, "os-arch %v not available. Available sfx platforms are:\n\n", wantPlat)
		for _, d := range dir {
			_, _ = fmt.Fprintf(os.Stderr, "* %s\n", strings.TrimSuffix(d.Name(), ".s2"))
		}
		_, _ = fmt.Fprintf(os.Stderr, "\nUse -os and -arch to specify the destination platform.")
		os.Exit(1)
	}
	exec, err = ioutil.ReadAll(s2.NewReader(bytes.NewBuffer(exec)))
	exitErr(err)

	mode := byte(opUnpack)
	if *untar {
		mode = opUnTar
	}
	if *stdout {
		// Write exec once to stdout
		_, err = os.Stdout.Write(exec)
		exitErr(err)
		_, err = os.Stdout.Write([]byte{mode})
		exitErr(err)
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
			var out io.Writer
			switch {
			case *stdout:
				out = os.Stdout
			default:
				if *safe {
					_, err := os.Stat(dstFilename)
					if !os.IsNotExist(err) {
						exitErr(errors.New("destination file exists"))
					}
				}
				dstFile, err := os.OpenFile(dstFilename, os.O_CREATE|os.O_WRONLY, 0777)
				exitErr(err)
				defer dstFile.Close()
				bw := bufio.NewWriterSize(dstFile, 4<<20*2)
				defer bw.Flush()
				out = bw
				_, err = out.Write(exec)
				exitErr(err)
				_, err = out.Write([]byte{mode})
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
