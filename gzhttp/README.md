Gzip Handler
============

This is a tiny Go package which wraps HTTP server handlers to transparently gzip the
response body, for clients which support it. 

For HTTP clients we provide a transport wrapper that will do gzip decompression 
faster than what the standard library offers.   

This package is forked from the dead [nytimes/gziphandler](https://github.com/nytimes/gziphandler)
and extends functionality for it.

## Install
```bash
go get -u github.com/klauspost/compress
```

## Documentation

[![Go Reference](https://pkg.go.dev/badge/github.com/klauspost/compress/gzhttp.svg)](https://pkg.go.dev/github.com/klauspost/compress/gzhttp)


## Usage

There are 2 main parts, one for http servers and one for http clients.

### Client

The standard library automatically adds gzip compression to most requests 
and handles decompression of the responses.

However, by wrapping the transport we are able to override this and provide 
our own (faster) decompressor.

Wrapping is done on the Transport of the http client:

```Go
func ExampleTransport() {
	// Get an HTTP client.
	client := http.Client{
		// Wrap the transport:
		Transport: gzhttp.Transport(http.DefaultTransport),
	}

	resp, err := client.Get("https://google.com")
	if err != nil {
		return
	}
    defer resp.Body.Close()
	
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("body:", string(body))
}
```

Speed compared to standard library for an approximate 127KB payload:

```
BenchmarkTransport

Single core:
BenchmarkTransport/gzhttp-32         	    1995	    609791 ns/op	 214.14 MB/s	   10129 B/op	      73 allocs/op
BenchmarkTransport/stdlib-32         	    1567	    772161 ns/op	 169.11 MB/s	   53950 B/op	      99 allocs/op

Multi Core:
BenchmarkTransport/gzhttp-par-32     	    8428	    199682 ns/op	 653.95 MB/s	   42332 B/op	     120 allocs/op
BenchmarkTransport/stdlib-par-32     	    3946	    311886 ns/op	 418.69 MB/s	   71972 B/op	     137 allocs/op
```

This includes both serving the http request, parsing requests and decompressing. 
For this payload size decompression is actually only about 40% of CPU load on the multi-core benchmark.

### Server

For the simplest usage call `GzipHandler` with any handler (an object which implements the
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


### Performance

Speed compared to  [nytimes/gziphandler](https://github.com/nytimes/gziphandler) with default settings:

```
λ benchcmp before.txt after.txt                                        
benchmark                         old ns/op     new ns/op     delta    
BenchmarkGzipHandler_S2k-32       51302         25554         -50.19%  
BenchmarkGzipHandler_S20k-32      301426        174900        -41.98%  
BenchmarkGzipHandler_S100k-32     1546203       912349        -40.99%  
BenchmarkGzipHandler_P2k-32       3973          2116          -46.74%  
BenchmarkGzipHandler_P20k-32      20319         12237         -39.78%  
BenchmarkGzipHandler_P100k-32     96079         57348         -40.31%  
                                                                       
benchmark                         old MB/s     new MB/s     speedup    
BenchmarkGzipHandler_S2k-32       39.92        80.14        2.01x      
BenchmarkGzipHandler_S20k-32      67.94        117.10       1.72x      
BenchmarkGzipHandler_S100k-32     66.23        112.24       1.69x      
BenchmarkGzipHandler_P2k-32       515.44       967.76       1.88x      
BenchmarkGzipHandler_P20k-32      1007.92      1673.55      1.66x      
BenchmarkGzipHandler_P100k-32     1065.79      1785.58      1.68x      
                                                                       
benchmark                         old allocs     new allocs     delta  
BenchmarkGzipHandler_S2k-32       22             19             -13.64%
BenchmarkGzipHandler_S20k-32      25             22             -12.00%
BenchmarkGzipHandler_S100k-32     28             25             -10.71%
BenchmarkGzipHandler_P2k-32       22             19             -13.64%
BenchmarkGzipHandler_P20k-32      25             22             -12.00%
BenchmarkGzipHandler_P100k-32     27             24             -11.11%
```

### Stateless compression

In cases where you expect to run many thousands of compressors concurrently, 
but with very little activity you can use stateless compression. 
This is not intended for regular web servers serving individual requests.

Use `CompressionLevel(-3)` or `CompressionLevel(gzip.StatelessCompression)` to enable.
Consider adding a [`bufio.Writer`](https://golang.org/pkg/bufio/#NewWriterSize) with a small buffer.

See [more details on stateless compression](https://github.com/klauspost/compress#stateless-compression).

### Migrating from gziphandler

This package removes some of the extra constructors.
When replacing, this can be used to find a replacement.

* `GzipHandler(h)` -> `GzipHandler(h)` (keep as-is)
* `GzipHandlerWithOpts(opts...)` -> `NewWrapper(opts...)`
* `MustNewGzipLevelHandler(n)` -> `NewWrapper(CompressionLevel(n))`
* `NewGzipLevelAndMinSize(n, s)` -> `NewWrapper(CompressionLevel(n), MinSize(s))` 

By default, some mime types will now be excluded.
To re-enable compression of all types, use the `ContentTypeFilter(gzhttp.CompressAllContentTypeFilter)` option.

## License

[Apache 2.0](LICENSE)


