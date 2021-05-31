Gzip Handler
============

This is a tiny Go package which wraps HTTP handlers to transparently gzip the
response body, for clients which support it. 

This package is forked from the dead [nytimes/gziphandler](https://github.com/nytimes/gziphandler)
and extends functionality for it.

## Install
```bash
go get -u github.com/klauspost/compress
```

## Documentation

[![Go Reference](https://pkg.go.dev/badge/github.com/klauspost/compress/gzhttp.svg)](https://pkg.go.dev/github.com/klauspost/compress/gzhttp)


## Usage

For the simplest usage call `MustGzipHandler` with any handler (an object which implements the
`http.Handler` interface), and it'll return a new handler which gzips the
response. For example:

```go
package main

import (
	"io"
	"net/http"
	"github.com/klauspost/compress/gzhttp"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "Hello, World")
	})
    
	http.Handle("/", gzhttp.MustGzipHandler(handler))
	http.ListenAndServe("0.0.0.0:8000", nil)
}
```

This will wrap a handler using the default options. 

To specify custom options a reusable wrapper can be created that can be used to wrap
any number of handlers.

```Go
package main

import (
	"io"
	"log"
	"net/http"
	
	"github.com/klauspost/compress/gzhttp"
	"github.com/klauspost/compress/gzip"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "Hello, World")
	})
	
   	// Create a reusable wrapper with custom options.
    wrapper, err := gzhttp.NewWrapper(gzhttp.MinSize(2000), gzhttp.CompressionLevel(gzip.BestSpeed))
	if err != nil {
		log.Fatalln(err)
	}
	
	http.Handle("/", wrapper(handler))
	http.ListenAndServe("0.0.0.0:8000", nil)
}
```

## License

[Apache 2.0](LICENSE)


