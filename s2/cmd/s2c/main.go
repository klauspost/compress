package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/s2/cmd/internal/readahead"
)

var (
	faster    = flag.Bool("faster", false, "Compress faster, but with a minor compression loss")
	slower    = flag.Bool("slower", false, "Compress more, but a lot slower")
	cpu       = flag.Int("cpu", runtime.GOMAXPROCS(0), "Compress using this amount of threads")
	blockSize = flag.String("blocksize", "4M", "Max  block size. Examples: 64K, 256K, 1M, 4M. Must be power of two and <= 4MB")
	safe      = flag.Bool("safe", false, "Do not overwrite output files")
	padding   = flag.String("pad", "1", "Pad size to a multiple of this value, Examples: 500, 64K, 256K, 1M, 4M, etc")
	stdout    = flag.Bool("c", false, "Write all output to stdout. Multiple input files will be concatenated")
	remove    = flag.Bool("rm", false, "Delete source file(s) after successful compression")
	quiet     = flag.Bool("q", false, "Don't write any output to terminal, except errors")
	bench     = flag.Int("bench", 0, "Run benchmark n times. No output will be written")
	help      = flag.Bool("help", false, "Display help")

	cpuprofile, memprofile, traceprofile string

	version = "(dev)"
	date    = "(unknown)"
)

func main() {
	if false {
		flag.StringVar(&cpuprofile, "cpuprofile", "", "write cpu profile to file")
		flag.StringVar(&memprofile, "memprofile", "", "write mem profile to file")
		flag.StringVar(&traceprofile, "traceprofile", "", "write trace profile to file")
	}
	flag.Parse()
	sz, err := toSize(*blockSize)
	exitErr(err)
	pad, err := toSize(*padding)
	exitErr(err)

	args := flag.Args()
	if len(args) == 0 || *help || (*slower && *faster) {
		_, _ = fmt.Fprintf(os.Stderr, "s2 compress v%v, built at %v.\n\n", version, date)
		_, _ = fmt.Fprintf(os.Stderr, "Copyright (c) 2011 The Snappy-Go Authors. All rights reserved.\n"+
			"Copyright (c) 2019 Klaus Post. All rights reserved.\n\n")
		_, _ = fmt.Fprintln(os.Stderr, `Usage: s2c [options] file1 file2

Compresses all files supplied as input separately.
Output files are written as 'filename.ext.s2'.
By default output files will be overwritten.
Use - as the only file name to read from stdin and write to stdout.

Wildcards are accepted: testdir/*.txt will compress all files in testdir ending with .txt
Directories can be wildcards as well. testdir/*/*.txt will match testdir/subdir/b.txt

Options:`)
		flag.PrintDefaults()
		os.Exit(0)
	}
	opts := []s2.WriterOption{s2.WriterBlockSize(int(sz)), s2.WriterConcurrency(*cpu), s2.WriterPadding(int(pad))}
	if !*faster {
		opts = append(opts, s2.WriterBetterCompression())
	}
	if *slower {
		opts = append(opts, s2.WriterBestCompression())
	}
	wr := s2.NewWriter(nil, opts...)

	// No args, use stdin/stdout
	if len(args) == 1 && args[0] == "-" {
		// Catch interrupt, so we don't exit at once.
		// os.Stdin will return EOF, so we should be able to get everything.
		signal.Notify(make(chan os.Signal), os.Interrupt)
		wr.Reset(os.Stdout)
		_, err = wr.ReadFrom(os.Stdin)
		printErr(err)
		printErr(wr.Close())
		return
	}
	var files []string

	for _, pattern := range args {
		found, err := filepath.Glob(pattern)
		exitErr(err)
		if len(found) == 0 {
			exitErr(fmt.Errorf("unable to find file %v", pattern))
		}
		files = append(files, found...)
	}
	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if memprofile != "" {
		f, err := os.Create(memprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		defer pprof.WriteHeapProfile(f)
	}
	if traceprofile != "" {
		f, err := os.Create(traceprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		err = trace.Start(f)
		if err != nil {
			log.Fatal(err)
		}
		defer trace.Stop()
	}

	*quiet = *quiet || *stdout
	if *bench > 0 {
		debug.SetGCPercent(10)
		for _, filename := range files {
			func() {
				if !*quiet {
					fmt.Print("Reading ", filename, "...")
				}
				// Input file.
				file, err := os.Open(filename)
				exitErr(err)
				finfo, err := file.Stat()
				exitErr(err)
				b := make([]byte, finfo.Size())
				_, err = io.ReadFull(file, b)
				exitErr(err)
				file.Close()
				for i := 0; i < *bench; i++ {
					wc := wCounter{out: ioutil.Discard}
					if !*quiet {
						fmt.Print("\nCompressing...")
					}
					wr.Reset(&wc)
					start := time.Now()
					err := wr.EncodeBuffer(b)
					exitErr(err)
					err = wr.Close()
					exitErr(err)
					if !*quiet {
						input := len(b)
						elapsed := time.Since(start)
						mbpersec := (float64(input) / (1024 * 1024)) / (float64(elapsed) / (float64(time.Second)))
						pct := float64(wc.n) * 100 / float64(input)
						ms := elapsed.Round(time.Millisecond)
						fmt.Printf(" %d -> %d [%.02f%%]; %v, %.01fMB/s", input, wc.n, pct, ms, mbpersec)
					}
				}
				fmt.Println("")
				wr.Close()
			}()
		}
		os.Exit(0)
	}

	for _, filename := range files {
		func() {
			var closeOnce sync.Once
			dstFilename := fmt.Sprintf("%s%s", filename, ".s2")
			if *bench > 0 {
				dstFilename = "(discarded)"
			}
			if !*quiet {
				fmt.Print("Compressing ", filename, " -> ", dstFilename)
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
			case *bench > 0:
				out = ioutil.Discard
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
				bw := bufio.NewWriterSize(dstFile, int(sz)*2)
				defer bw.Flush()
				out = bw
			}
			wc := wCounter{out: out}
			wr.Reset(&wc)
			defer wr.Close()
			start := time.Now()
			input, err := wr.ReadFrom(src)
			exitErr(err)
			err = wr.Close()
			exitErr(err)
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

func printErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nERROR:", err.Error())
	}
}

func exitErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nERROR:", err.Error())
		os.Exit(2)
	}
}

// toSize converts a size indication to bytes.
func toSize(size string) (uint64, error) {
	size = strings.ToUpper(strings.TrimSpace(size))
	firstLetter := strings.IndexFunc(size, unicode.IsLetter)
	if firstLetter == -1 {
		firstLetter = len(size)
	}

	bytesString, multiple := size[:firstLetter], size[firstLetter:]
	bytes, err := strconv.ParseUint(bytesString, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unable to parse size: %v", err)
	}

	switch multiple {
	case "M", "MB", "MIB":
		return bytes * 1 << 20, nil
	case "K", "KB", "KIB":
		return bytes * 1 << 10, nil
	case "B", "":
		return bytes, nil
	default:
		return 0, fmt.Errorf("unknown size suffix: %v", multiple)
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
