# S2 Compression

S2 is an extension of [Snappy](https://github.com/google/snappy).

Decoding is compatible with Snappy compressed content, but content compressed with S2 cannot be decompressed by Snappy.

This means that S2 can seamlessly replace Snappy without converting compressed content.

S2 is aimed for high throughput, which is also why it features concurrent compression for bigger payloads.

# Extensions

* Frame [Stream identifier](https://github.com/google/snappy/blob/master/framing_format.txt#L68) changed from `sNaPpY` to `S2sTwO`.
* [Framed compressed blocks](https://github.com/google/snappy/blob/master/format_description.txt) can be up to 1MB (up from 64KB).
* Compressed blocks can have an offset of `0`, which indicates to repeat the last seen offset.

Repeat offsets must be encoded as a [2.2.1. Copy with 1-byte offset (01)](https://github.com/google/snappy/blob/master/format_description.txt#L89), where the offset is 0.

The length is specified by reading the 3-bit length specified in the tag and decode using this table:

| Length | Actual Length        |
|--------|----------------------|
| 0      | 4                    |
| 1      | 5                    |
| 2      | 6                    |
| 3      | 7                    |
| 4      | 8                    |
| 5      | 8 + read 1 byte      |
| 6      | 260 + read 2 bytes   |
| 7      | 65540 + read 3 bytes |

This allows any repeat offset + length to be represented by 2 to 5 bytes.

Lengths are stored as little endian values.

Initial repeat offset of a block is '1'.

# Performance

Compression is increased, mostly around 5-20% and the throughput is typically 25-40% increased (single threaded) compared to the non-assembly Go implementation.

A "better" compression mode is also available. This allows to trade a bit of speed for a minor compression gain.
The content compressed in this mode is fully compatible with the standard decoder.

Snappy vs S2 compression speed on 6 core (12 thread) computer, using all threads and a single thread:

| File                        | S2 speed | S2 throughput | S2 % smaller | S2 decomp | S2 "better" | "better" throughput | "better" % smaller | "better" decomp |
|-----------------------------|----------|---------------|--------------|-----------|-------------|---------------------|--------------------|-----------------|
| [rawstudio-mint14.tar](https://files.klauspost.com/compress/rawstudio-mint14.7z)         | 6.64x    | 3067 MB/s     | 7.13%        | 1.23x     | 3.49x       | 1612 MB/s           | 10.76%             | 0.96x           |
| (1 CPU)                     | 1.19x    | 548 MB/s      |             | 2145 MB/s | 0.59x       | 273 MB/s            |                   | 1679 MB/s       |
| [github-june-2days-2019.json](https://files.klauspost.com/compress/github-june-2days-2019.json.zst) | 7.03x    | 3550 MB/s     | 28.79%       | 1.40x     | 5.91x       | 2985 MB/s           | 30.80%             | 1.24x           |
| (1 CPU)                     | 1.37x    | 689 MB/s      |             | 2473 MB/s | 1.00x       | 505 MB/s            |                   | 2193 MB/s       |
| [github-ranks-backup.bin](https://files.klauspost.com/compress/github-ranks-backup.bin.zst)     | 5.66x    | 2720 MB/s     | -5.90%       | 1.10x     | 4.08x       | 1962 MB/s           | 4.23%              | 0.92x           |
| (1 CPU)                     | 1.10x    | 529 MB/s      |             | 2082 MB/s | 0.71x       | 341 MB/s            |                   | 1745 MB/s       |
| [consensus.db.10gb](https://files.klauspost.com/compress/consensus.db.10gb.zst)           | 6.90x    | 3279 MB/s     | 14.80%       | 1.11x     | 3.48x       | 1657 MB/s           | 14.78%             | 1.10x           |
| (1 CPU)                     | 1.28x    | 608 MB/s      |             | 2973 MB/s | 0.63x       | 298 MB/s            |                   | 2942 MB/s       |
| [adresser.json](https://files.klauspost.com/compress/adresser.json.zst)               | 4.08x    | 3919 MB/s     | 43.52%       | 1.27x     | 3.98x       | 3831 MB/s           | 45.85%             | 1.22x           |
| (1 CPU)                     | 1.59x    | 1529 MB/s     |             | 4288 MB/s | 1.39x       | 1335 MB/s           |                   | 4105 MB/s       |
| [gob-stream](https://files.klauspost.com/compress/gob-stream.7z)                   | 5.82x    | 2939 MB/s     | 22.24%       | 1.26x     | 5.18x       | 2618 MB/s           | 24.63%             | 1.09x           |
| (1 CPU)                     | 1.32x    | 667 MB/s      |             | 2307 MB/s | 0.99x       | 500 MB/s            |                   | 1987 MB/s       |
| [10gb.tar](http://mattmahoney.net/dc/10gb.html)                     | 6.56x    | 2647 MB/s     | 2.26%        | 1.09x     | 4.36x       | 1760 MB/s           | 5.64%              | 0.93x           |
| (1 CPU)                     | 1.07x    | 433 MB/s      |             | 1540 MB/s | 0.76x       | 307 MB/s            |                   | 1322 MB/s       |
| sharnd.out.2gb              | 0.90x    | 3799 MB/s     | 0.01%        | 0.84x     | 0.88x       | 3730 MB/s           | 0.01%              | 0.85x           |
| (1 CPU)                     | 0.97x    | 4103 MB/s     |             | 3470 MB/s | 0.96x       | 4071 MB/s           |                   | 3518 MB/s       |
| [enwik9](http://mattmahoney.net/dc/textdata.html)                     | 8.23x    | 1907 MB/s     | 3.67%        | 1.39x     | 5.30x       | 1229 MB/s           | 14.36%             | 1.05x           |
| (1 CPU)                     | 1.31x    | 303 MB/s      |             | 1253 MB/s | 0.86x       | 199 MB/s            |                   | 947 MB/s        |
| [silesia.tar](http://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip)                 | 5.27x    | 1643 MB/s     | 5.84%        | 1.19x     | 3.64x       | 1135 MB/s           | 12.06%             | 0.98x           |
| (1 CPU)                     | 1.23x    | 383 MB/s      |             | 1338 MB/s | 0.77x       | 239 MB/s            |                   | 1098 MB/s       |

### Legend

* `S2 speed`: Speed of S2 compared to Snappy, using 6 cores and 1 core.
* `S2 throughput`: Throughput of S2 in MB/s. 
* `S2 % smaller`: How many percent of the Snappy output size is S2 better.
* `S2 decomp`: Decompression speed of S2 compared to Snappy and absolute speed.
* `S2 "better"`: Speed when enabling "better" compression mode in S2 compared to Snappy. 
* `"better" throughput`: Speed when enabling "better" compression mode in S2 compared to Snappy. 
* `"better" % smaller`: How many percent of the Snappy output size is S2 better when using "better" compression.
* `"better" decomp`: Decompression speed of S2 "better" mode compared to Snappy and absolute speed.

There is a good speedup across the board when using a single thread and a significant speedup when using multiple threads.

Machine generated data gets by far the biggest compression boost, with size being being reduced by up to 45% of Snappy size.

The "better" compression mode sees a good improvement in all cases, but usually at a performance cost.
 

## Concurrent Stream Compression

Streams are concurrently compressed. The stream will be distributed among all available CPU cores for the best possible throughput.

Snappy vs S2 compression speed on 6 core (12 thread) computer:

| File                        | S2 throughput | S2 % Smaller | S2 throughput |
|-----------------------------|--------------|--------------|---------------|
| consensus.db.10gb           | 7.33x        | 14.70%       | 3595.97 MB/s  |
| github-ranks-backup.bin     | 6.22x        | -9.39%       | 2964.83 MB/s  |
| github-june-2days-2019.json | 7.48x        | 28.80%       | 3741.06 MB/s  |
| rawstudio-mint14.tar        | 7.35x        | 6.34%        | 3398.61 MB/s  |
| 10gb.tar                    | 6.99x        | 1.75%        | 2819.25 MB/s  |
| enwik9                      | 8.85x        | 3.63%        | 2050.45 MB/s  |
| sharnd.out.2gb              | 0.91x        | 0.01%        | 3770.79 MB/s  |
| adresser.json               | 4.10x        | 45.94%       | 3937.66 MB/s  |
| silesia.tar                 | 5.30x        | 5.21%        | 1656.42 MB/s  |

Incompressible content (`sharnd.out.2gb`, 2GB random data) sees the smallest speedup. This is likely dominated by synchronization overhead.

## Decompression

While the decompression code hasn't changed, there is a significant speedup in decompression speed.

Decompression remains close to original Snappy speed, with a single additional branch for 1 byte offset matches. So only minor differences should be assumed there.
Only if your decompression platform is heavily memory limited, will there be a difference.

Single goroutine decompression speed. No assembly:

| File                           | S2 Throughput | S2 throughput |
|--------------------------------|--------------|---------------|
| consensus.db.10gb.s2           | 1.84x        | 2289.8 MB/s   |
| 10gb.tar.s2                    | 1.30x        | 867.07 MB/s   |
| rawstudio-mint14.tar.s2        | 1.66x        | 1329.65 MB/s  |
| github-june-2days-2019.json.s2 | 2.36x        | 1831.59 MB/s  |
| github-ranks-backup.bin.s2     | 1.73x        | 1390.7 MB/s   |
| enwik9.s2                      | 1.67x        | 681.53 MB/s   |
| adresser.json.s2               | 3.41x        | 4230.53 MB/s  |
| silesia.tar.s2                 | 1.52x        | 811.58        |


Single goroutine decompression speed. With AMD64 assembly:

| File                           | S2 throughput | S2 throughput |
|--------------------------------|--------------|---------------|
| consensus.db.10gb.s2           | 1.15x        | 3074 MB/s     |
| 10gb.tar.s2                    | 1.08x        | 1534 MB/s     |
| rawstudio-mint14.tar.s2        | 1.27x        | 2220 MB/s     |
| github-june-2days-2019.json.s2 | 1.40x        | 2468 MB/s     |
| github-ranks-backup.bin.s2     | 1.11x        | 2132 MB/s     |
| enwik9.s2                      | 1.42x        | 1280 MB/s     |
| adresser.json.s2               | 1.34x        | 4550 MB/s     |
| silesia.tar.s2                 | 1.22x        | 1374 MB/s     |

Even though S2 typically compresses better than Snappy, decompression speed is always better. 

# LICENSE

This code is based on the [Snappy-Go](https://github.com/golang/snappy) implementation.

Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.
