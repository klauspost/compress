# compress

This package is based on an optimized Deflate function, which is used by gzip/zip/zlib packages.

It offers slightly better compression at lower compression settings, and up to 3x faster encoding at highest compression level.

* [High Throughput Benchmark](http://blog.klauspost.com/go-gzipdeflate-benchmarks/).
* [Small Payload/Webserver Benchmarks](http://blog.klauspost.com/gzip-performance-for-go-webservers/).
* [Constant Time Compression](http://blog.klauspost.com/constant-time-gzipzip-compression/).

[![Build Status](https://travis-ci.org/klauspost/compress.svg?branch=master)](https://travis-ci.org/klauspost/compress)

# usage

The packages are drop-in replacements for standard libraries. Simply replace the import path to use them:

| old import         | new import                              |
|--------------------|-----------------------------------------|
| `compress/gzip`    | `github.com/klauspost/compress/gzip`    |
| `compress/zlib`    | `github.com/klauspost/compress/zlib`    |
| `archive/zip`      | `github.com/klauspost/compress/zip`     |
| `compress/deflate` | `github.com/klauspost/compress/deflate` |

You may also be interested in [pgzip](https://github.com/klauspost/pgzip), which is a drop in replacement for gzip, which support multithreaded compression on big files and the optimized [crc32](https://github.com/klauspost/crc32) package used by these packages.

The packages contains the same as the standard library, so you can use the godoc for that: [gzip](http://golang.org/pkg/compress/gzip/), [zip](http://golang.org/pkg/archive/zip/),  [zlib](http://golang.org/pkg/compress/zlib/), [flate](http://golang.org/pkg/compress/flate/).

Currently there is only minor speedup on decompression (primarily CRC32 calculation).

# deflate optimizations

* Minimum matches are 4 bytes, this leads to fewer searches and better compression.
* Stronger hash (iSCSI CRC32) for matches on x64 with SSE 4.2 support. This leads to fewer hash collisions.
* Literal byte matching using SSE 4.2 for faster string comparisons.
* Bulk hashing on matches.
* Much faster dictionary indexing with `NewWriterDict()`/`Reset()`.
* Make Bit Coder faster by assuming we are on a 64 bit CPU.
* Level 1 compression replaced by converted "Snappy" algorithm.


```
benchmark                              old ns/op     new ns/op     delta
BenchmarkEncodeDigitsSpeed1e4-4        1384066       242354        -82.49%
BenchmarkEncodeDigitsSpeed1e5-4        4680153       2255480       -51.81%
BenchmarkEncodeDigitsSpeed1e6-4        38413996      22487169      -41.46%
BenchmarkEncodeDigitsDefault1e4-4      1569564       455660        -70.97%
BenchmarkEncodeDigitsDefault1e5-4      16352741      6163936       -62.31%
BenchmarkEncodeDigitsDefault1e6-4      174296430     65393170      -62.48%
BenchmarkEncodeDigitsCompress1e4-4     1563731       449619        -71.25%
BenchmarkEncodeDigitsCompress1e5-4     16327115      6238991       -61.79%
BenchmarkEncodeDigitsCompress1e6-4     174614590     66643880      -61.83%
BenchmarkEncodeTwainSpeed1e4-4         1448479       257964        -82.19%
BenchmarkEncodeTwainSpeed1e5-4         4580435       2054643       -55.14%
BenchmarkEncodeTwainSpeed1e6-4         36429376      20353457      -44.13%
BenchmarkEncodeTwainDefault1e4-4       1642155       504991        -69.25%
BenchmarkEncodeTwainDefault1e5-4       13942046      6845407       -50.90%
BenchmarkEncodeTwainDefault1e6-4       142828830     73648630      -48.44%
BenchmarkEncodeTwainCompress1e4-4      1621868       501998        -69.05%
BenchmarkEncodeTwainCompress1e5-4      17421421      7589886       -56.43%
BenchmarkEncodeTwainCompress1e6-4      184582970     81904175      -55.63%

benchmark                              old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4-4        7.23         41.26        5.71x
BenchmarkEncodeDigitsSpeed1e5-4        21.37        44.34        2.07x
BenchmarkEncodeDigitsSpeed1e6-4        26.03        44.47        1.71x
BenchmarkEncodeDigitsDefault1e4-4      6.37         21.95        3.45x
BenchmarkEncodeDigitsDefault1e5-4      6.12         16.22        2.65x
BenchmarkEncodeDigitsDefault1e6-4      5.74         15.29        2.66x
BenchmarkEncodeDigitsCompress1e4-4     6.39         22.24        3.48x
BenchmarkEncodeDigitsCompress1e5-4     6.12         16.03        2.62x
BenchmarkEncodeDigitsCompress1e6-4     5.73         15.01        2.62x
BenchmarkEncodeTwainSpeed1e4-4         6.90         38.76        5.62x
BenchmarkEncodeTwainSpeed1e5-4         21.83        48.67        2.23x
BenchmarkEncodeTwainSpeed1e6-4         27.45        49.13        1.79x
BenchmarkEncodeTwainDefault1e4-4       6.09         19.80        3.25x
BenchmarkEncodeTwainDefault1e5-4       7.17         14.61        2.04x
BenchmarkEncodeTwainDefault1e6-4       7.00         13.58        1.94x
BenchmarkEncodeTwainCompress1e4-4      6.17         19.92        3.23x
BenchmarkEncodeTwainCompress1e5-4      5.74         13.18        2.30x
BenchmarkEncodeTwainCompress1e6-4      5.42         12.21        2.25x
```
* "Speed" is compression level 1
* "Default" is compression level 6
* "Compress" is compression level 9
* Test files are [Digits](https://github.com/klauspost/compress/blob/master/testdata/e.txt) (no matches) and [Twain](https://github.com/klauspost/compress/blob/master/testdata/Mark.Twain-Tom.Sawyer.txt) (plain text) .

As can be seen speed on low-matching souces `Digits` are a tiny bit slower at compression level 1, but for default compression it shows a very good speedup.

`Twain` is a much more realistic benchmark, and will be closer to JSON/HTML performance. Here speed is equivalent or faster, up to 2 times.

**Without assembly**. This is what you can expect on systems that does not have amd64 and SSE 4:
```
benchmark                              old ns/op     new ns/op     delta
BenchmarkEncodeDigitsSpeed1e4-4        1384066       251761        -81.81%
BenchmarkEncodeDigitsSpeed1e5-4        4680153       2355534       -49.67%
BenchmarkEncodeDigitsSpeed1e6-4        38413996      22744953      -40.79%
BenchmarkEncodeDigitsDefault1e4-4      1569564       620075        -60.49%
BenchmarkEncodeDigitsDefault1e5-4      16352741      10236776      -37.40%
BenchmarkEncodeDigitsDefault1e6-4      174296430     124677990     -28.47%
BenchmarkEncodeDigitsCompress1e4-4     1563731       602055        -61.50%
BenchmarkEncodeDigitsCompress1e5-4     16327115      10466877      -35.89%
BenchmarkEncodeDigitsCompress1e6-4     174614590     110307460     -36.83%
BenchmarkEncodeTwainSpeed1e4-4         1448479       260372        -82.02%
BenchmarkEncodeTwainSpeed1e5-4         4580435       2139385       -53.29%
BenchmarkEncodeTwainSpeed1e6-4         36429376      20903734      -42.62%
BenchmarkEncodeTwainDefault1e4-4       1642155       660436        -59.78%
BenchmarkEncodeTwainDefault1e5-4       13942046      10376797      -25.57%
BenchmarkEncodeTwainDefault1e6-4       142828830     107270570     -24.90%
BenchmarkEncodeTwainCompress1e4-4      1621868       635404        -60.82%
BenchmarkEncodeTwainCompress1e5-4      17421421      11347456      -34.86%
BenchmarkEncodeTwainCompress1e6-4      184582970     121477220     -34.19%

benchmark                              old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4-4        7.23         39.72        5.49x
BenchmarkEncodeDigitsSpeed1e5-4        21.37        42.45        1.99x
BenchmarkEncodeDigitsSpeed1e6-4        26.03        43.97        1.69x
BenchmarkEncodeDigitsDefault1e4-4      6.37         16.13        2.53x
BenchmarkEncodeDigitsDefault1e5-4      6.12         9.77         1.60x
BenchmarkEncodeDigitsDefault1e6-4      5.74         8.02         1.40x
BenchmarkEncodeDigitsCompress1e4-4     6.39         16.61        2.60x
BenchmarkEncodeDigitsCompress1e5-4     6.12         9.55         1.56x
BenchmarkEncodeDigitsCompress1e6-4     5.73         9.07         1.58x
BenchmarkEncodeTwainSpeed1e4-4         6.90         38.41        5.57x
BenchmarkEncodeTwainSpeed1e5-4         21.83        46.74        2.14x
BenchmarkEncodeTwainSpeed1e6-4         27.45        47.84        1.74x
BenchmarkEncodeTwainDefault1e4-4       6.09         15.14        2.49x
BenchmarkEncodeTwainDefault1e5-4       7.17         9.64         1.34x
BenchmarkEncodeTwainDefault1e6-4       7.00         9.32         1.33x
BenchmarkEncodeTwainCompress1e4-4      6.17         15.74        2.55x
BenchmarkEncodeTwainCompress1e5-4      5.74         8.81         1.53x
BenchmarkEncodeTwainCompress1e6-4      5.42         8.23         1.52x
```
## Modified Level 1 compression

Level 1 "BestSpeed" is completely replaced by a converted version of the algorithm found in Snappy.
This version is considerably faster than the "old" deflate at level 1. It does however come at a compression loss, usually in the order of 3-4% compared to the old level 1. However, the speed is usually 1.75 times that of the fastest deflate mode.

In my previous experiments the most common case for "level 1" was that it provided no significant speedup, only lower compression compared to level 2 and sometimes even 3.

However, the modified Snappy algorithm provides a very good sweet spot. Usually about 75% faster and with only little compression loss. Therefore I decided to *replace* level 1 with this mode entirely.


## Compression level

This table shows the compression at each level, and the percentage of the output size compared to output
at the similar level with the standard library. Compression data is `Twain`, see above.

| Level | Bytes  | % size |
|-------|--------|--------|
| 1     | 194622 | 103.7% |
| 2     | 174684 | 96.85% |
| 3     | 170301 | 98.45% |
| 4     | 165253 | 97.69% |
| 5     | 161274 | 98.65% |
| 6     | 160464 | 99.71% |
| 7     | 160304 | 99.87% |
| 8     | 160279 | 99.99% |
| 9     | 160279 | 99.99% |

To interpret and example, this version of deflate compresses input of 407287 bytes to 161274 bytes at level 5, which is 98.6% of the size of what the standard library produces; 161274 bytes.

This means that from level 2-5 you can expect a compression level increase of a few percent. Level 1 is about 3% worse, as descibed above.

# linear time compression

This compression library adds a special compression level, named `ConstantCompression`, which allows near linear time compression. This is done by completely disabling matching of previous data, and only reduce the number of bits to represent each character. 

This means that often used characters, like 'e' and ' ' (space) in text use the fewest bits to represent, and rare characters like '¤' takes more bits to represent. For more information see [wikipedia](https://en.wikipedia.org/wiki/Huffman_coding) or this nice [video](https://youtu.be/ZdooBTdW5bM).

Since this type of compression has much less variance, the compression speed is mostly unaffected by the input data, and is usually more than *150MB/s* for a single core.

The downside is that the compression ratio is usually considerably worse than even the fastest conventional compression. The compression raio can never be better than 8:1 (12.5%). 

The linear time compression can be used as a "better than nothing" mode, where you cannot risk the encoder to slow down on some content. For comparison, the size of the "Twain" text is *233460 bytes* (+29% vs. level 1) and encode speed is 144MB/s (4.5x level 1). So in this case you trade a 30% size increase for a 4 times speedup.

For more information see my blog post on [Fast Linear Time Compression](http://blog.klauspost.com/constant-time-gzipzip-compression/).

# gzip/zip optimizations
 * Uses the faster deflate
 * Uses SSE 4.2 CRC32 calculations.

Speed increase is up to 3x of the standard library, but usually around 30%. Without SSE 4.2, speed is roughly equivalent, but compression should be slightly better.

This is close to a real world benchmark as you will get. A 2.3MB JSON file.
```
benchmark             old ns/op     new ns/op     delta
BenchmarkGzipL1-4     95212470      59938275      -37.05%
BenchmarkGzipL2-4     102069730     76349195      -25.20%
BenchmarkGzipL3-4     115472770     82492215      -28.56%
BenchmarkGzipL4-4     153197780     107570890     -29.78%
BenchmarkGzipL5-4     203930260     134387930     -34.10%
BenchmarkGzipL6-4     233172100     145495400     -37.60%
BenchmarkGzipL7-4     297190260     197926950     -33.40%
BenchmarkGzipL8-4     512819750     376244733     -26.63%
BenchmarkGzipL9-4     563366800     403266833     -28.42%

benchmark             old MB/s     new MB/s     speedup
BenchmarkGzipL1-4     52.11        82.78        1.59x
BenchmarkGzipL2-4     48.61        64.99        1.34x
BenchmarkGzipL3-4     42.97        60.15        1.40x
BenchmarkGzipL4-4     32.39        46.13        1.42x
BenchmarkGzipL5-4     24.33        36.92        1.52x
BenchmarkGzipL6-4     21.28        34.10        1.60x
BenchmarkGzipL7-4     16.70        25.07        1.50x
BenchmarkGzipL8-4     9.68         13.19        1.36x
BenchmarkGzipL9-4     8.81         12.30        1.40x
```

Multithreaded compression using [pgzip](https://github.com/klauspost/pgzip) comparison, Quadcore, CPU = 8:

```
benchmark           old ns/op     new ns/op     delta
BenchmarkGzipL1     96155500      25981486      -72.98%
BenchmarkGzipL2     101905830     24601408      -75.86%
BenchmarkGzipL3     113506490     26321506      -76.81%
BenchmarkGzipL4     143708220     31761818      -77.90%
BenchmarkGzipL5     188210770     39602266      -78.96%
BenchmarkGzipL6     209812000     40402313      -80.74%
BenchmarkGzipL7     270015440     56103210      -79.22%
BenchmarkGzipL8     461359700     91255220      -80.22%
BenchmarkGzipL9     498361833     88755075      -82.19%

benchmark           old MB/s     new MB/s     speedup
BenchmarkGzipL1     51.60        190.97       3.70x
BenchmarkGzipL2     48.69        201.69       4.14x
BenchmarkGzipL3     43.71        188.51       4.31x
BenchmarkGzipL4     34.53        156.22       4.52x
BenchmarkGzipL5     26.36        125.29       4.75x
BenchmarkGzipL6     23.65        122.81       5.19x
BenchmarkGzipL7     18.38        88.44        4.81x
BenchmarkGzipL8     10.75        54.37        5.06x
BenchmarkGzipL9     9.96         55.90        5.61x
```
