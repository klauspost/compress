//go:build custom
// +build custom

package main

import (
	"bytes"
	"flag"
	"log"
	"os"

	"github.com/klauspost/asmfmt"
)

func main() {
	flag.Parse()
	args := flag.Args()
	for _, file := range args {
		data, err := os.ReadFile(file)
		if err != nil {
			log.Fatalln(err)
		}
		data = bytes.Replace(data, []byte("\t// #"), []byte("#"), -1)
		data, err = asmfmt.Format(bytes.NewBuffer(data))
		if err != nil {
			log.Fatalln(err)
		}
		err = os.WriteFile(file, data, os.ModePerm)
		if err != nil {
			log.Fatalln(err)
		}
	}
}
