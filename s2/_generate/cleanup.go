//go:build custom
// +build custom

package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"os"

	"github.com/klauspost/asmfmt"
)

func main() {
	flag.Parse()
	args := flag.Args()
	for _, file := range args {
		data, err := ioutil.ReadFile(file)
		if err != nil {
			log.Fatalln(err)
		}
		data = bytes.Replace(data, []byte("\t// #"), []byte("#"), -1)
		data, err = asmfmt.Format(bytes.NewBuffer(data))
		if err != nil {
			log.Fatalln(err)
		}
		err = ioutil.WriteFile(file, data, os.ModePerm)
		if err != nil {
			log.Fatalln(err)
		}
	}
}
