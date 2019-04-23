# zstd 

[Zstandard](https://facebook.github.io/zstd/) is a real-time compression algorithm, providing high compression ratios. 
It offers a very wide range of compression / speed trade-off, while being backed by a very fast decoder. 

Currently this package provides *decompression* of zstandard compressed content. 
Note that custom dictionaries are not supported yet, so if your code relies on that, 
you cannot use the package as-is.

This package is pure Go and without use of "unsafe".

The `zstd` package is provided as open source software using a Go standard license.


## Decompressor

STATUS: BETA - there may still be subtle bugs, but a wide variety of content has been tested.

 
### Usage

Install using `go get -u github.com/klauspost/compress`. The package is located in `github.com/klauspost/compress/zstd`.

Godoc Documentation: https://godoc.org/github.com/klauspost/compress/zstd

You will also need the `github.com/cespare/xxhash` package.

The package has been designed for two main usages, big streams of data and smaller in-memory buffers. 
There are two main usages of the package for these. Both of them are accessed by creating a `Decoder`.

For streaming use a simple setup could look like this:

```Go
import "github.com/klauspost/compress/zstd"

func Decompress(in io.Reader, out io.Writer) error {
    d, err := zstd.NewReader(input)
    if err != nil {
    	return err
    }
    defer d.Close()
    
    // Copy content...
    _, err := io.Copy(out, d)
    return err
}
```

It is important to use the "Close" function when you no longer need the Reader to stop running goroutines. 
See "Allocation-less operation" below.

For decoding buffers, it could look something like this:

```Go
import "github.com/klauspost/compress/zstd"

// Create a reader that caches decompressors.
// For this operation type we supply a nil Reader.
var decoder, _ = zstd.NewReader(nil)

// Decompress a buffer. We don't supply a destination buffer,
// so it will be allocated by the decoder.
func Decompress(src []byte) ([]byte, error) {
	return decoder.DecodeAll(src, nil)
} 
```

Both of these cases should provide the functionality needed. 
The decoder can be used for *concurrent* decompression of multiple buffers. 
It will only allow a certain number of concurrent operations to run. 
To tweak that yourself use the `WithDecoderConcurrency(n)` option when creating the decoder.   

### Allocation-less operation

The decoder has been designed to operate without allocations after a warmup. 

This means that you should *store* the decoder for best performance. 
To re-use a stream decoder, use the `Reset(r io.Reader) error` to switch to another stream.
A decoder can safely be re-used even if the previous stream failed.

To release the resources, you must call the `Close()` function on a decoder.
After this it can *no longer be reused*, but all running goroutines will be stopped.
So you *must* use this if you will no longer need the Reader.

For decompressing smaller buffers a single decoder can be used.
When decoding buffers, you can supply a destination slice with length 0 and your expected capacity.
In this case no unneeded allocations should be made. 

### Concurrency

The buffer decoder does everything on the same goroutine and does nothing concurrently.
It can however decode several buffers concurrently. Use `WithDecoderConcurrency(n)` to limit that.

The stream decoder operates on

* One goroutine reads input and splits the input to several block decoders.
* A number of decoders will decode blocks.
* A goroutine coordinates these blocks and sends history from one to the next.

So effectively this also means the decoder will "read ahead" and prepare data to always be available for output.

Since "blocks" are quite dependent on the output of the previous block stream decoding will only have limited concurrency.

In practice this means that concurrency is often limited to utilizing about 2 cores effectively.
 
 
### Benchmarks

These are some examples of performance compared to [datadog cgo library](https://github.com/DataDog/zstd).

The first two are streaming decodes and the last are smaller inputs. 
 
```
BenchmarkDecoderSilesia-8             20       642550210 ns/op   329.85 MB/s      3101 B/op        8 allocs/op
BenchmarkDecoderSilesiaCgo-8         100       384930000 ns/op   550.61 MB/s    451878 B/op     9713 allocs/op

BenchmarkDecoderEnwik9-2              10        3146000080 ns/op         317.86 MB/s        2649 B/op          9 allocs/op
BenchmarkDecoderEnwik9Cgo-2           20        1905900000 ns/op         524.69 MB/s     1125120 B/op      45785 allocs/op

BenchmarkDecoder_DecodeAll/z000000.zst-8               200     7049994 ns/op   138.26 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000001.zst-8            100000       19560 ns/op    97.49 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000002.zst-8              5000      297599 ns/op   236.99 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000003.zst-8              2000      725502 ns/op   141.17 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000004.zst-8            200000        9314 ns/op    54.54 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000005.zst-8             10000      137500 ns/op   104.72 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000006.zst-8               500     2316009 ns/op   206.06 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000007.zst-8             20000       64499 ns/op   344.90 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000008.zst-8             50000       24900 ns/op   219.56 MB/s        40 B/op        2 allocs/op
BenchmarkDecoder_DecodeAll/z000009.zst-8              1000     2348999 ns/op   154.01 MB/s        40 B/op        2 allocs/op

BenchmarkDecoder_DecodeAllCgo/z000000.zst-8            500     4268005 ns/op   228.38 MB/s   1228849 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000001.zst-8         100000       15250 ns/op   125.05 MB/s      2096 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000002.zst-8          10000      147399 ns/op   478.49 MB/s     73776 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000003.zst-8           5000      320798 ns/op   319.27 MB/s    139312 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000004.zst-8         200000       10004 ns/op    50.77 MB/s       560 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000005.zst-8          20000       73599 ns/op   195.64 MB/s     19120 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000006.zst-8           1000     1119003 ns/op   426.48 MB/s    557104 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000007.zst-8          20000      103450 ns/op   215.04 MB/s     71296 B/op        9 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000008.zst-8         100000       20130 ns/op   271.58 MB/s      6192 B/op        3 allocs/op
BenchmarkDecoder_DecodeAllCgo/z000009.zst-8           2000     1123500 ns/op   322.00 MB/s    368688 B/op        3 allocs/op
```

This reflects the performance around May 2019, but this may be out of date.

# Contributions

Contributions are always welcome. 
For new features/fixes, remember to add tests and for performance enhancements include benchmarks.

For sending files for reproducing errors use a service like [goobox](https://goobox.io/#/upload) or similar to share your files.