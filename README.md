# compress

This package is based on an optimized Deflate function, which is used by gzip/zip packages.

It offers slightly better compression at lower compression settings, and up to 3x faster encoding at highest compression level.

[![Build Status](https://travis-ci.org/klauspost/compress.svg?branch=master)](https://travis-ci.org/klauspost/compress)

# usage

The packages are drop-in replacements for standard libraries. Simply replace the import path to use them:

| old import         | new import                              |
|--------------------|-----------------------------------------|
| `compress/gzip`    | `github.com/klauspost/compress/gzip`    |
| `archive/zip`      | `github.com/klauspost/compress/zip`     |
| `compress/deflate` | `github.com/klauspost/compress/deflate` |

You may also be interested in [pgzip](https://github.com/klauspost/pgzip), which is a drop in replacement for gzip, which support multithreaded compression on big files and the optimized [crc32](https://github.com/klauspost/crc32) package used by these packages.

# deflate optimizations

* Minimum matches are 4 bytes, this leads to fewer searches and better compression.
* Stronger hash (iSCSI CRC32) for matches on x64 with SSE 4.1 support. This leads to fewer hash collisions.
* Literal byte matching using SSE 4.2 for faster string comparisons.
* Bulk hashing on matches.
* Much faster dictionary indexing with `NewWriterDict()`/`Reset()`.
* Make Bit Coder faster by assuming we are on a 64 bit CPU.


```
BenchmarkEncodeDigitsSpeed1e4        571065        571799        +0.13%
BenchmarkEncodeDigitsSpeed1e5        3680010       4645932       +26.25%
BenchmarkEncodeDigitsSpeed1e6        34667982      45532604      +31.34%
BenchmarkEncodeDigitsDefault1e4      770694        619535        -19.61%
BenchmarkEncodeDigitsDefault1e5      13682782      6032845       -55.91%
BenchmarkEncodeDigitsDefault1e6      152778738     61443514      -59.78%
BenchmarkEncodeDigitsCompress1e4     771094        620635        -19.51%
BenchmarkEncodeDigitsCompress1e5     13683782      5999343       -56.16%
BenchmarkEncodeDigitsCompress1e6     152648731     61228502      -59.89%
BenchmarkEncodeTwainSpeed1e4         595100        570165        -4.19%
BenchmarkEncodeTwainSpeed1e5         3432796       3376593       -1.64%
BenchmarkEncodeTwainSpeed1e6         31573806      30687755      -2.81%
BenchmarkEncodeTwainDefault1e4       828697        674388        -18.62%
BenchmarkEncodeTwainDefault1e5       11572161      6733885       -41.81%
BenchmarkEncodeTwainDefault1e6       122607013     68998946      -43.72%
BenchmarkEncodeTwainCompress1e4      833297        679738        -18.43%
BenchmarkEncodeTwainCompress1e5      14539831      7372921       -49.29%
BenchmarkEncodeTwainCompress1e6      160019152     77099410      -51.82%

benchmark                            old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4        17.51        17.49        1.00x
BenchmarkEncodeDigitsSpeed1e5        27.17        21.52        0.79x
BenchmarkEncodeDigitsSpeed1e6        28.85        21.96        0.76x
BenchmarkEncodeDigitsDefault1e4      12.98        16.14        1.24x
BenchmarkEncodeDigitsDefault1e5      7.31         16.58        2.27x
BenchmarkEncodeDigitsDefault1e6      6.55         16.28        2.49x
BenchmarkEncodeDigitsCompress1e4     12.97        16.11        1.24x
BenchmarkEncodeDigitsCompress1e5     7.31         16.67        2.28x
BenchmarkEncodeDigitsCompress1e6     6.55         16.33        2.49x
BenchmarkEncodeTwainSpeed1e4         16.80        17.54        1.04x
BenchmarkEncodeTwainSpeed1e5         29.13        29.62        1.02x
BenchmarkEncodeTwainSpeed1e6         31.67        32.59        1.03x
BenchmarkEncodeTwainDefault1e4       12.07        14.83        1.23x
BenchmarkEncodeTwainDefault1e5       8.64         14.85        1.72x
BenchmarkEncodeTwainDefault1e6       8.16         14.49        1.78x
BenchmarkEncodeTwainCompress1e4      12.00        14.71        1.23x
BenchmarkEncodeTwainCompress1e5      6.88         13.56        1.97x
BenchmarkEncodeTwainCompress1e6      6.25         12.97        2.08x
```
* "Speed" is compression level 1
* "Default" is compression level 6
* "Compress" is compression level 9
* Test files are [Digits](https://github.com/klauspost/compress/blob/master/testdata/e.txt) (no matches) and [Twain](https://github.com/klauspost/compress/blob/master/testdata/Mark.Twain-Tom.Sawyer.txt) (plain text) .

As can be seen speed on low-matching souces `Digits` are a tiny bit slower at compression level 1, but for default compression it shows a very good speedup.

`Twain` is a much more realistic benchmark, and will be closer to JSON/HTML performance. Here speed is equivalent or faster, up to 2 times.

Without assembly. This is what you can expect on systems that does not have amd64 and SSE 4.2:
```
benchmark                            old ns/op     new ns/op     delta
BenchmarkEncodeDigitsSpeed1e4        571065        647787        +13.43%
BenchmarkEncodeDigitsSpeed1e5        3680010       5925338       +61.01%
BenchmarkEncodeDigitsSpeed1e6        34667982      59040043      +70.30%
BenchmarkEncodeDigitsDefault1e4      770694        723391        -6.14%
BenchmarkEncodeDigitsDefault1e5      13682782      9633051       -29.60%
BenchmarkEncodeDigitsDefault1e6      152778738     102595868     -32.85%
BenchmarkEncodeDigitsCompress1e4     771094        724141        -6.09%
BenchmarkEncodeDigitsCompress1e5     13683782      9589048       -29.92%
BenchmarkEncodeDigitsCompress1e6     152648731     102295851     -32.99%
BenchmarkEncodeTwainSpeed1e4         595100        620835        +4.32%
BenchmarkEncodeTwainSpeed1e5         3432796       4013029       +16.90%
BenchmarkEncodeTwainSpeed1e6         31573806      37160125      +17.69%
BenchmarkEncodeTwainDefault1e4       828697        774044        -6.60%
BenchmarkEncodeTwainDefault1e5       11572161      9537045       -17.59%
BenchmarkEncodeTwainDefault1e6       122607013     99745705      -18.65%
BenchmarkEncodeTwainCompress1e4      833297        784094        -5.90%
BenchmarkEncodeTwainCompress1e5      14539831      10679610      -26.55%
BenchmarkEncodeTwainCompress1e6      160019152     113616498     -29.00%

benchmark                            old MB/s     new MB/s     speedup
BenchmarkEncodeDigitsSpeed1e4        17.51        15.44        0.88x
BenchmarkEncodeDigitsSpeed1e5        27.17        16.88        0.62x
BenchmarkEncodeDigitsSpeed1e6        28.85        16.94        0.59x
BenchmarkEncodeDigitsDefault1e4      12.98        13.82        1.06x
BenchmarkEncodeDigitsDefault1e5      7.31         10.38        1.42x
BenchmarkEncodeDigitsDefault1e6      6.55         9.75         1.49x
BenchmarkEncodeDigitsCompress1e4     12.97        13.81        1.06x
BenchmarkEncodeDigitsCompress1e5     7.31         10.43        1.43x
BenchmarkEncodeDigitsCompress1e6     6.55         9.78         1.49x
BenchmarkEncodeTwainSpeed1e4         16.80        16.11        0.96x
BenchmarkEncodeTwainSpeed1e5         29.13        24.92        0.86x
BenchmarkEncodeTwainSpeed1e6         31.67        26.91        0.85x
BenchmarkEncodeTwainDefault1e4       12.07        12.92        1.07x
BenchmarkEncodeTwainDefault1e5       8.64         10.49        1.21x
BenchmarkEncodeTwainDefault1e6       8.16         10.03        1.23x
BenchmarkEncodeTwainCompress1e4      12.00        12.75        1.06x
BenchmarkEncodeTwainCompress1e5      6.88         9.36         1.36x
BenchmarkEncodeTwainCompress1e6      6.25         8.80         1.41x
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

Speed increase is up to 3x of the standard library, but usually around 30%. Without SSE 4.1, speed is roughly equivalent, but compression should be slightly better.

This is close to a real world benchmark as you will get. A 2.3MB JSON file.
```
benchmark           old ns/op     new ns/op     delta
BenchmarkGzipL1     95035436      71914113      -24.33%
BenchmarkGzipL2     100665758     74774276      -25.72%
BenchmarkGzipL3     111666387     80764620      -27.67%
BenchmarkGzipL4     141848114     101145785     -28.69%
BenchmarkGzipL5     185630618     127187274     -31.48%
BenchmarkGzipL6     207511870     137047840     -33.96%
BenchmarkGzipL7     265115163     183970522     -30.61%
BenchmarkGzipL8     454926020     348619940     -23.37%
BenchmarkGzipL9     488327935     377671600     -22.66%

benchmark           old MB/s     new MB/s     speedup
BenchmarkGzipL1     52.21        69.00        1.32x
BenchmarkGzipL2     49.29        66.36        1.35x
BenchmarkGzipL3     44.43        61.43        1.38x
BenchmarkGzipL4     34.98        49.06        1.40x
BenchmarkGzipL5     26.73        39.01        1.46x
BenchmarkGzipL6     23.91        36.20        1.51x
BenchmarkGzipL7     18.72        26.97        1.44x
BenchmarkGzipL8     10.91        14.23        1.30x
BenchmarkGzipL9     10.16        13.14        1.29x
```

Multithreaded compression using [pgzip](https://github.com/klauspost/pgzip) comparison, Quadcore, CPU = 8:

```
benchmark           old ns/op     new ns/op     delta
BenchmarkGzipL1     95035436      30381737      -68.03%
BenchmarkGzipL2     100665758     31341793      -68.87%
BenchmarkGzipL3     111666387     32891881      -70.54%
BenchmarkGzipL4     141848114     41767389      -70.55%
BenchmarkGzipL5     185630618     47742730      -74.28%
BenchmarkGzipL6     207511870     50272875      -75.77%
BenchmarkGzipL7     265115163     62693586      -76.35%
BenchmarkGzipL8     454926020     107436145     -76.38%
BenchmarkGzipL9     488327935     114066524     -76.64%

benchmark           old MB/s     new MB/s     speedup
BenchmarkGzipL1     52.21        163.31       3.13x
BenchmarkGzipL2     49.29        158.31       3.21x
BenchmarkGzipL3     44.43        150.85       3.40x
BenchmarkGzipL4     34.98        118.80       3.40x
BenchmarkGzipL5     26.73        103.93       3.89x
BenchmarkGzipL6     23.91        98.70        4.13x
BenchmarkGzipL7     18.72        79.14        4.23x
BenchmarkGzipL8     10.91        46.18        4.23x
BenchmarkGzipL9     10.16        43.50        4.28x
```
