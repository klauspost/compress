Gzip Handler
============

This is a tiny Go package which wraps HTTP handlers to transparently gzip the
response body, for clients which support it. 

This package is forked from the [dead nytimes/gziphandler](https://github.com/nytimes/gziphandler)
and extends functionality for it.

## Install
```bash
go get -u github.com/klauspost/compress
```

## Usage

Call `GzipHandler` with any handler (an object which implements the
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
    
	http.Handle("/", gzhttp.GzipHandler(handler))
	http.ListenAndServe("0.0.0.0:8000", nil)
}
```


## Documentation

The docs can be found at [godoc.org][], as usual.


## License

[Apache 2.0][license].



[docs]:     https://godoc.org/github.com/NYTimes/gziphandler
[license]:  https://github.com/NYTimes/gziphandler/blob/master/LICENSE
