// Copyright 2023+ Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/dict"
)

var (
	wantLenFlag   = flag.Int("len", 112<<10, "Specify custom output size")
	wantHashBytes = flag.Int("hash", 6, "Hash bytes match length. Minimum match length.")
	wantMaxBytes  = flag.Int("max", 32<<10, "Max input length to index per input file")
	wantOutput    = flag.String("o", "dictionary.bin", "Output name")
	wantFormat    = flag.String("format", "zstd", `Output type. "zstd" "s2" or "raw"`)
	wantZstdID    = flag.Uint("zstdid", 0, "Zstd dictionary ID. 0 will be random")
	quiet         = flag.Bool("q", false, "Do not print progress")
)

func main() {
	flag.Parse()
	o := dict.Options{
		MaxDictSize: *wantLenFlag,
		HashBytes:   *wantHashBytes,
		Output:      os.Stdout,
		ZstdDictID:  uint32(*wantZstdID),
	}
	if *wantOutput == "" || *quiet {
		o.Output = nil
	}
	var input [][]byte
	base := flag.Arg(0)
	if base == "" {
		log.Fatal("no path with files specified")
	}

	// Index ALL hashes in all files.
	filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			log.Print(err)
			return nil
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, int64(*wantMaxBytes)))
		if len(b) < 8 {
			return nil
		}
		input = append(input, b)
		if !*quiet {
			fmt.Print("\r"+info.Name(), " read...")
		}
		return nil
	})
	var out []byte
	var err error
	switch *wantFormat {
	case "zstd":
		out, err = dict.BuildZstdDict(input, o)
	case "s2":
		out, err = dict.BuildS2Dict(input, o)
	case "raw":
		out, err = dict.BuildRawDict(input, o)
	default:
		err = fmt.Errorf("unknown format %q", *wantFormat)
	}
	if err != nil {
		log.Fatal(err)
	}
	if *wantOutput != "" {
		err = os.WriteFile(*wantOutput, out, 0666)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		_, err = os.Stdout.Write(out)
		if err != nil {
			log.Fatal(err)
		}
	}
}
