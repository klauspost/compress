# compress

This package is based on an optimized Deflate function, which is used by gzip/zip/zlib packages.

It offers slightly better compression at lower compression settings, and up to 3x faster encoding at highest compression level.

* [High Throughput Benchmark](http://blog.klauspost.com/go-gzipdeflate-benchmarks/).
* Small payload Benchmarks- coming soon.

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


```
benchmark                            old ns/op     new ns/op     delta
BenchmarkEncodeDigitsSpeed1e4        574032        391822        -31.74%
BenchmarkEncodeDigitsSpeed1e5        3634207       4260243       +17.23%
BenchmarkEncodeDigitsSpeed1e6        34501974      43035796      +24.73%
BenchmarkEncodeDigitsDefault1e4      813046        434024        -46.62%
BenchmarkEncodeDigitsDefault1e5      14030802      5553651       -60.42%
BenchmarkEncodeDigitsDefault1e6      155308880     58503350      -62.33%
BenchmarkEncodeDigitsCompress1e4     789545        433691        -45.07%
BenchmarkEncodeDigitsCompress1e5     14110807      5680325       -59.74%
BenchmarkEncodeDigitsCompress1e6     154308830     59653410      -61.34%
BenchmarkEncodeTwainSpeed1e4         607034        367221        -39.51%
BenchmarkEncodeTwainSpeed1e5         3458197       3042174       -12.03%
BenchmarkEncodeTwainSpeed1e6         31841820      28901652      -9.23%
BenchmarkEncodeTwainDefault1e4       833047        471026        -43.46%
BenchmarkEncodeTwainDefault1e5       11690669      6245357       -46.58%
BenchmarkEncodeTwainDefault1e6       124307110     64903715      -47.79%
BenchmarkEncodeTwainCompress1e4      841048        475360        -43.48%
BenchmarkEncodeTwainCompress1e5      14620836      6870393       -53.01%
BenchmarkEncodeTwainCompress1e6      161409230     74254250      -54.00%

benchmark                            old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4        17.42        25.52        1.46x
BenchmarkEncodeDigitsSpeed1e5        27.52        23.47        0.85x
BenchmarkEncodeDigitsSpeed1e6        28.98        23.24        0.80x
BenchmarkEncodeDigitsDefault1e4      12.30        23.04        1.87x
BenchmarkEncodeDigitsDefault1e5      7.13         18.01        2.53x
BenchmarkEncodeDigitsDefault1e6      6.44         17.09        2.65x
BenchmarkEncodeDigitsCompress1e4     12.67        23.06        1.82x
BenchmarkEncodeDigitsCompress1e5     7.09         17.60        2.48x
BenchmarkEncodeDigitsCompress1e6     6.48         16.76        2.59x
BenchmarkEncodeTwainSpeed1e4         16.47        27.23        1.65x
BenchmarkEncodeTwainSpeed1e5         28.92        32.87        1.14x
BenchmarkEncodeTwainSpeed1e6         31.41        34.60        1.10x
BenchmarkEncodeTwainDefault1e4       12.00        21.23        1.77x
BenchmarkEncodeTwainDefault1e5       8.55         16.01        1.87x
BenchmarkEncodeTwainDefault1e6       8.04         15.41        1.92x
BenchmarkEncodeTwainCompress1e4      11.89        21.04        1.77x
BenchmarkEncodeTwainCompress1e5      6.84         14.56        2.13x
BenchmarkEncodeTwainCompress1e6      6.20         13.47        2.17x
```
* "Speed" is compression level 1
* "Default" is compression level 6
* "Compress" is compression level 9
* Test files are [Digits](https://github.com/klauspost/compress/blob/master/testdata/e.txt) (no matches) and [Twain](https://github.com/klauspost/compress/blob/master/testdata/Mark.Twain-Tom.Sawyer.txt) (plain text) .

As can be seen speed on low-matching souces `Digits` are a tiny bit slower at compression level 1, but for default compression it shows a very good speedup.

`Twain` is a much more realistic benchmark, and will be closer to JSON/HTML performance. Here speed is equivalent or faster, up to 2 times.

**Without assembly**. This is what you can expect on systems that does not have amd64 and SSE 4:
```
benchmark                            old ns/op     new ns/op     delta
BenchmarkEncodeDigitsSpeed1e4        574032        468026        -18.47%
BenchmarkEncodeDigitsSpeed1e5        3634207       5553651       +52.82%
BenchmarkEncodeDigitsSpeed1e6        34501974      56253220      +63.04%
BenchmarkEncodeDigitsDefault1e4      813046        541030        -33.46%
BenchmarkEncodeDigitsDefault1e5      14030802      9020516       -35.71%
BenchmarkEncodeDigitsDefault1e6      155308880     97755590      -37.06%
BenchmarkEncodeDigitsCompress1e4     789545        543697        -31.14%
BenchmarkEncodeDigitsCompress1e5     14110807      9040517       -35.93%
BenchmarkEncodeDigitsCompress1e6     154308830     98005610      -36.49%
BenchmarkEncodeTwainSpeed1e4         607034        427024        -29.65%
BenchmarkEncodeTwainSpeed1e5         3458197       3654209       +5.67%
BenchmarkEncodeTwainSpeed1e6         31841820      35182014      +10.49%
BenchmarkEncodeTwainDefault1e4       833047        581699        -30.17%
BenchmarkEncodeTwainDefault1e5       11690669      8935511       -23.57%
BenchmarkEncodeTwainDefault1e6       124307110     95505460      -23.17%
BenchmarkEncodeTwainCompress1e4      841048        590533        -29.79%
BenchmarkEncodeTwainCompress1e5      14620836      10085576      -31.02%
BenchmarkEncodeTwainCompress1e6      161409230     109806280     -31.97%

benchmark                            old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4        17.42        21.37        1.23x
BenchmarkEncodeDigitsSpeed1e5        27.52        18.01        0.65x
BenchmarkEncodeDigitsSpeed1e6        28.98        17.78        0.61x
BenchmarkEncodeDigitsDefault1e4      12.30        18.48        1.50x
BenchmarkEncodeDigitsDefault1e5      7.13         11.09        1.56x
BenchmarkEncodeDigitsDefault1e6      6.44         10.23        1.59x
BenchmarkEncodeDigitsCompress1e4     12.67        18.39        1.45x
BenchmarkEncodeDigitsCompress1e5     7.09         11.06        1.56x
BenchmarkEncodeDigitsCompress1e6     6.48         10.20        1.57x
BenchmarkEncodeTwainSpeed1e4         16.47        23.42        1.42x
BenchmarkEncodeTwainSpeed1e5         28.92        27.37        0.95x
BenchmarkEncodeTwainSpeed1e6         31.41        28.42        0.90x
BenchmarkEncodeTwainDefault1e4       12.00        17.19        1.43x
BenchmarkEncodeTwainDefault1e5       8.55         11.19        1.31x
BenchmarkEncodeTwainDefault1e6       8.04         10.47        1.30x
BenchmarkEncodeTwainCompress1e4      11.89        16.93        1.42x
BenchmarkEncodeTwainCompress1e5      6.84         9.92         1.45x
BenchmarkEncodeTwainCompress1e6      6.20         9.11         1.47x
```

## Compression level

This table shows the compression at each level, and the percentage of the output size compared to output
at the similar level with the standard library. Compression data is `Twain`, see above.

| Level | Bytes  | % size |
|-------|--------|--------|
| 1     | 180539 | 96.24% |
| 2     | 174684 | 96.85% |
| 3     | 170301 | 98.45% |
| 4     | 165253 | 97.69% |
| 5     | 161274 | 98.65% |
| 6     | 160464 | 99.71% |
| 7     | 160304 | 99.87% |
| 8     | 160279 | 99.99% |
| 9     | 160279 | 99.99% |

To interpret and example, this version of deflate compresses input of 407287 bytes to 180539 bytes at level 1, which is 96% of the size of what the standard library produces; 187563 bytes.

This means that from level 1-5 you can expect a compression level increase of a few percent.

# gzip/zip optimizations
 * Uses the faster deflate
 * Uses SSE 4.2 CRC32 calculations.

Speed increase is up to 3x of the standard library, but usually around 30%. Without SSE 4.2, speed is roughly equivalent, but compression should be slightly better.

This is close to a real world benchmark as you will get. A 2.3MB JSON file.
```
benchmark           old ns/op     new ns/op     delta
BenchmarkGzipL1     96155500      69203960      -28.03%
BenchmarkGzipL2     101905830     71304080      -30.03%
BenchmarkGzipL3     113506490     77754450      -31.50%
BenchmarkGzipL4     143708220     97105555      -32.43%
BenchmarkGzipL5     188210770     123207050     -34.54%
BenchmarkGzipL6     209812000     129607410     -38.23%
BenchmarkGzipL7     270015440     176510100     -34.63%
BenchmarkGzipL8     461359700     328218760     -28.86%
BenchmarkGzipL9     498361833     355687000     -28.63%

benchmark           old MB/s     new MB/s     speedup
BenchmarkGzipL1     51.60        71.70        1.39x
BenchmarkGzipL2     48.69        69.59        1.43x
BenchmarkGzipL3     43.71        63.81        1.46x
BenchmarkGzipL4     34.53        51.10        1.48x
BenchmarkGzipL5     26.36        40.27        1.53x
BenchmarkGzipL6     23.65        38.28        1.62x
BenchmarkGzipL7     18.38        28.11        1.53x
BenchmarkGzipL8     10.75        15.12        1.41x
BenchmarkGzipL9     9.96         13.95        1.40x
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
