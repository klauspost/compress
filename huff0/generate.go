//go:build ignore
// +build ignore

package main

import (
	"log"
	"os"
	"path"
	"text/template"
)

func main() {
	mapping := []struct {
		template string
		output   string
	}{{
		template: "decompress_amd64.s.in",
		output:   "decompress_amd64.s",
	},
		{
			template: "decompress_8b_amd64.s.in",
			output:   "decompress_8b_amd64.s",
		},
	}

	for i := range mapping {

		state := make(map[string]string)

		funcMap := template.FuncMap{
			"var": func(name string) string { return state[name] },
			"set": func(name, value string) string {
				state[name] = value
				return ""
			},
		}

		input := mapping[i].template
		output := mapping[i].output
		if !shouldRegenerate(input, output) {
			log.Printf("%q is up to date", output)
			continue
		}

		tmpl, err := template.New(path.Base(input)).Funcs(funcMap).ParseFiles(input)
		die(err)

		f, err := os.Create(output)
		die(err)
		defer f.Close()

		log.Printf("Generating %q from %q", output, input)
		err = tmpl.Execute(f, nil)
		die(err)
	}
}

func die(err error) {
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}

func shouldRegenerate(srcpath, dstpath string) bool {
	src, err1 := os.Stat(srcpath)
	if err1 != nil {
		return true // I/O errors will be rediscovered later
	}

	dst, err2 := os.Stat(dstpath)
	if err2 != nil {
		return true
	}

	return src.ModTime().After(dst.ModTime())
}
