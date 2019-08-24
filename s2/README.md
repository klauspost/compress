# S2 Compression

S2 is an extension of [Snappy](https://github.com/google/snappy).

Decoding is compatible with Snappy compressed content, but content compressed with S2 cannot be decompressed by Snappy.

This means that S2 can seamlessly replace Snappy without converting compressed content.

S2 is aimed for high throughput, which is also why it features concurrent compression for bigger payloads.

## Benefits over Snappy

* Better compression
* Concurrent stream compression
* Faster decompression
* Ability to quickly skip forward in compressed stream
* Compatible with Snappy compressed content
* Smaller maximum block size overhead
* Block concatenation
* Automatic stream size padding.

## Drawbacks over Snappy

* Not optimized for 32 bit systems.
* Uses slightly more memory due to larger blocks and concurrency (configurable).

# Format Extensions

* Frame [Stream identifier](https://github.com/google/snappy/blob/master/framing_format.txt#L68) changed from `sNaPpY` to `S2sTwO`.
* [Framed compressed blocks](https://github.com/google/snappy/blob/master/format_description.txt) can be up to 4MB (up from 64KB).
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

The first copy of a block cannot be a repeat offset.

Default streaming block size is 1MB.

# Concatenating blocks and streams.

Concatenating streams and blocks will concatenate the output of both without recompressing them. 
While this is inefficient in terms of compression it might be usable in certain scenarios. 

Streams can also be safely concatenated. 
The 10 byte 'stream identifier' of the second stream can optionally be stripped, but it is not a requirement.

Blocks can be concatenated using the `ConcatBlocks` function.

Snappy blocks/streams can safely be concatenated with S2 blocks and streams. 

# Performance

Compression is increased, mostly around 5-20% and the throughput is typically 25-40% increased (single threaded) compared to the non-assembly Go implementation.

A "better" compression mode is also available. This allows to trade a bit of speed for a minor compression gain.
The content compressed in this mode is fully compatible with the standard decoder.

Snappy vs S2 compression speed on 6 core (12 thread) computer, using all threads and a single thread:

| File                                                                                                | S2 speed | S2 throughput | S2 % smaller | S2 decomp | S2 "better" | "better" throughput | "better" % smaller | "better" decomp |
|-----------------------------------------------------------------------------------------------------|----------|---------------|--------------|-----------|-------------|---------------------|--------------------|-----------------|
| [rawstudio-mint14.tar](https://files.klauspost.com/compress/rawstudio-mint14.7z)                    | 7.41x    | 3401 MB/s     | 6.98%        | 1.19x     | 3.85x       | 1765 MB/s           | 10.66%             | 0.94x           |
| (1 CPU)                                                                                             | 1.37x    | 631 MB/s      | -            | 2085 MB/s | 0.70x       | 323 MB/s            | -                  | 1656 MB/s       |
| [github-june-2days-2019.json](https://files.klauspost.com/compress/github-june-2days-2019.json.zst) | 8.76x    | 4351 MB/s     | 28.79%       | 1.29x     | 6.56x       | 3258 MB/s           | 30.80%             | 1.14x           |
| (1 CPU)                                                                                             | 1.62x    | 806 MB/s      | -            | 2262 MB/s | 1.10x       | 547 MB/s            | -                  | 1990 MB/s       |
| [github-ranks-backup.bin](https://files.klauspost.com/compress/github-ranks-backup.bin.zst)         | 7.70x    | 3610 MB/s     | -5.90%       | 1.07x     | 4.74x       | 2223 MB/s           | 4.23%              | 0.86x           |
| (1 CPU)                                                                                             | 1.26x    | 592 MB/s      | -            | 2053 MB/s | 0.76x       | 356 MB/s            | -                  | 1663 MB/s       |
| [consensus.db.10gb](https://files.klauspost.com/compress/consensus.db.10gb.zst)                     | 7.17x    | 3494 MB/s     | 14.83%       | 1.03x     | 3.46x       | 1686 MB/s           | 14.78%             | 1.03x           |
| (1 CPU)                                                                                             | 1.41x    | 687 MB/s      | -            | 2805 MB/s | 0.64x       | 311 MB/s            | -                  | 2804 MB/s       |
| [adresser.json](https://files.klauspost.com/compress/adresser.json.zst)                             | 5.16x    | 4923 MB/s     | 43.52%       | 1.12x     | 5.34x       | 5098 MB/s           | 45.85%             | 1.09x           |
| (1 CPU)                                                                                             | 1.76x    | 1675 MB/s     | -            | 3791 MB/s | 1.50x       | 1431 MB/s           | -                  | 3718 MB/s       |
| [gob-stream](https://files.klauspost.com/compress/gob-stream.7z)                                    | 8.84x    | 4402 MB/s     | 22.24%       | 1.16x     | 6.80x       | 3387 MB/s           | 24.63%             | 1.01x           |
| (1 CPU)                                                                                             | 1.50x    | 747 MB/s      | -            | 2175 MB/s | 1.08x       | 537 MB/s            | -                  | 1902 MB/s       |
| [10gb.tar](http://mattmahoney.net/dc/10gb.html)                                                     | 6.73x    | 2715 MB/s     | 1.99%        | 1.04x     | 4.47x       | 1803 MB/s           | 5.48%              | 0.90x           |
| (1 CPU)                                                                                             | 1.17x    | 472 MB/s      | -            | 1493 MB/s | 0.81x       | 326 MB/s            | -                  | 1288 MB/s       |
| sharnd.out.2gb                                                                                      | 0.94x    | 5987 MB/s     | 0.01%        | 0.89x     | 0.90x       | 5768 MB/s           | 0.01%              | 0.90x           |
| (1 CPU)                                                                                             | 1.30x    | 8323 MB/s     | -            | 4222 MB/s | 1.18x       | 7528 MB/s           | -                  | 4266 MB/s       |
| [enwik9](http://mattmahoney.net/dc/textdata.html)                                                   | 10.02x   | 2337 MB/s     | 3.66%        | 1.35x     | 5.92x       | 1380 MB/s           | 14.36%             | 1.01x           |
| (1 CPU)                                                                                             | 1.38x    | 321 MB/s      | -            | 1230 MB/s | 0.86x       | 201 MB/s            | -                  | 917 MB/s        |
| [silesia.tar](http://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip)                                    | 7.72x    | 2464 MB/s     | 5.63%        | 1.22x     | 4.46x       | 1423 MB/s           | 11.89%             | 0.97x           |
| (1 CPU)                                                                                             | 1.31x    | 418 MB/s      | -            | 1454 MB/s | 0.80x       | 255 MB/s            | -                  | 1161 MB/s       |

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

We only compare to the non-assembly AMD64 version of Snappy, since S2 does not have an assembly implementation yet.
While this may seem to favour S2 on this platform, it is reasonable to assume that an S2 assembly implementation will gain the same speed.
Therefore comparing to the non-assembly version gives the best apples-to-apples comparison. 

## Concurrent Stream Compression

Streams are concurrently compressed. The stream will be distributed among all available CPU cores for the best possible throughput.

Snappy vs S2 compression speed on 6 core (12 thread) computer:

| File                        | S2 throughput | S2 % Smaller | S2 throughput |
|-----------------------------|--------------|--------------|---------------|
| consensus.db.10gb           | 7.33x        | 14.70%       | 3595 MB/s  |
| github-ranks-backup.bin     | 7.70x        | -5.90%       | 3610 MB/s  |
| github-june-2days-2019.json | 8.76x        | 28.79%       | 4351 MB/s  |
| rawstudio-mint14.tar        | 7.35x        | 6.98%        | 3401 MB/s  |
| 10gb.tar                    | 6.99x        | 1.99%        | 2819 MB/s  |
| enwik9                      | 10.02x       | 3.66%        | 2337 MB/s  |
| sharnd.out.2gb              | 0.94x        | 0.01%        | 5987 MB/s  |
| adresser.json               | 5.16x        | 45.94%       | 4923 MB/s  |
| silesia.tar                 | 7.72x        | 5.63%        | 2464 MB/s  |

Incompressible content (`sharnd.out.2gb`, 2GB random data) sees the smallest speedup. 
This is likely dominated by synchronization overhead, which is confirmed by the fact that single threaded performance is higher (see above). 

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

## Block compression

When compressing blocks no concurrent compression is performed just as Snappy. 
This is because blocks are for smaller payloads and generally will not benefit from concurrent compression.

Benchmarking single block performance is subject to a lot more variation since it only tests a limited number of file patterns.
So individual benchmarks should only be seen as a guideline and the overall picture is more important.

An important change is that incompressible blocks will not be more than at most 10 bytes bigger than the input.
In rare, worst case scenario Snappy blocks could be significantly bigger than the input.  

### Standard compression

| Absolute Perf  | Snappy size | S2 Size | Snappy Speed | Snappy Asm | S2 Speed   | Snappy dec | S2 dec     |
|----------------|-------------|---------|--------------|------------|------------|------------|------------|
| html           | 22843       | 21094   | 583 MB/s     | 1150 MB/s  | 814 MB/s   | 2870 MB/s  | 3830 MB/s  |
| urls.10K       | 335492      | 286835  | 336 MB/s     | 514 MB/s   | 472 MB/s   | 1600 MB/s  | 1960 MB/s  |
| fireworks.jpeg | 123034      | 123100  | 25900 MB/s   | 24800 MB/s | 21600 MB/s | 56900 MB/s | 63500 MB/s |
| paper-100k.pdf | 85304       | 84237   | 5160 MB/s    | 12400 MB/s | 4820 MB/s  | 24500 MB/s | 22300 MB/s |
| html_x_4       | 92234       | 21101   | 570 MB/s     | 1080 MB/s  | 2340 MB/s  | 2330 MB/s  | 2370 MB/s  |
| alice29.txt    | 88034       | 85934   | 218 MB/s     | 425 MB/s   | 292 MB/s   | 965 MB/s   | 1470 MB/s  |
| asyoulik.txt   | 77503       | 79569   | 208 MB/s     | 392 MB/s   | 301 MB/s   | 922 MB/s   | 1670 MB/s  |
| lcet10.txt     | 234661      | 220408  | 229 MB/s     | 427 MB/s   | 298 MB/s   | 1030 MB/s  | 1420 MB/s  |
| plrabn12.txt   | 319267      | 318197  | 199 MB/s     | 354 MB/s   | 277 MB/s   | 873 MB/s   | 1490 MB/s  |
| geo.protodata  | 23335       | 18696   | 743 MB/s     | 1560 MB/s  | 1160 MB/s  | 3820 MB/s  | 4990 MB/s  |
| kppkn.gtb      | 69526       | 65109   | 335 MB/s     | 682 MB/s   | 413 MB/s   | 1300 MB/s  | 1470 MB/s  |


| Relative Perf  | Snappy size | Asm speed | S2 size improved | S2 Speed | S2 Speed vs. asm | S2 Dec Speed |
|----------------|-------------|-----------|------------------|----------|------------------|--------------|
| html           | 22.31%      | 1.97x     | 7.66%            | 1.40x    | 0.71x            | 1.33x        |
| urls.10K       | 47.78%      | 1.53x     | 14.50%           | 1.40x    | 0.92x            | 1.23x        |
| fireworks.jpeg | 99.95%      | 0.96x     | -0.05%           | 0.83x    | 0.87x            | 1.12x        |
| paper-100k.pdf | 83.30%      | 2.40x     | 1.25%            | 0.93x    | 0.39x            | 0.91x        |
| html_x_4       | 22.52%      | 1.89x     | 77.12%           | 4.11x    | 2.17x            | 1.02x        |
| alice29.txt    | 57.88%      | 1.95x     | 2.39%            | 1.34x    | 0.69x            | 1.52x        |
| asyoulik.txt   | 61.91%      | 1.88x     | -2.67%           | 1.45x    | 0.77x            | 1.81x        |
| lcet10.txt     | 54.99%      | 1.86x     | 6.07%            | 1.30x    | 0.70x            | 1.38x        |
| plrabn12.txt   | 66.26%      | 1.78x     | 0.34%            | 1.39x    | 0.78x            | 1.71x        |
| geo.protodata  | 19.68%      | 2.10x     | 19.88%           | 1.56x    | 0.74x            | 1.31x        |
| kppkn.gtb      | 37.72%      | 2.04x     | 6.35%            | 1.23x    | 0.61x            | 1.13x        |

In almost all cases there is a size improvement and decompression speed is better, except 1 case.

Except for the PDF and JPEG (mostly incompressible content), the speed is better than the pure Go Snappy. 
However, looking at the absolute numbers, these cases are still above 4GB/s.

The worst case compression speed of Snappy (`plrabn12.txt` and `asyoulik.txt`) is significantly improved.

### Better compression

| Absolute Perf  | Snappy size | Better Size | Snappy Speed | Snappy Asm | Better Speed | Snappy dec | Better dec |
|----------------|-------------|-------------|--------------|------------|--------------|------------|------------|
| html           | 22843       | 19867       | 583 MB/s     | 1150 MB/s  | 560 MB/s     | 2870 MB/s  | 3100 MB/s  |
| urls.10K       | 335492      | 256907      | 336 MB/s     | 514 MB/s   | 291 MB/s     | 1600 MB/s  | 1470 MB/s  |
| fireworks.jpeg | 123034      | 123100      | 25900 MB/s   | 24800 MB/s | 9760 MB/s    | 56900 MB/s | 63300 MB/s |
| paper-100k.pdf | 85304       | 82915       | 5160 MB/s    | 12400 MB/s | 594 MB/s     | 24500 MB/s | 15000 MB/s |
| html_x_4       | 92234       | 19875       | 570 MB/s     | 1080 MB/s  | 1750 MB/s    | 2330 MB/s  | 2290 MB/s  |
| alice29.txt    | 88034       | 73429       | 218 MB/s     | 425 MB/s   | 203 MB/s     | 965 MB/s   | 1290 MB/s  |
| asyoulik.txt   | 77503       | 66997       | 208 MB/s     | 392 MB/s   | 186 MB/s     | 922 MB/s   | 1120 MB/s  |
| lcet10.txt     | 234661      | 191969      | 229 MB/s     | 427 MB/s   | 214 MB/s     | 1030 MB/s  | 1230 MB/s  |
| plrabn12.txt   | 319267      | 272575      | 199 MB/s     | 354 MB/s   | 179 MB/s     | 873 MB/s   | 1000 MB/s  |
| geo.protodata  | 23335       | 18303       | 743 MB/s     | 1560 MB/s  | 818 MB/s     | 3820 MB/s  | 4510 MB/s  |
| kppkn.gtb      | 69526       | 61992       | 335 MB/s     | 682 MB/s   | 310 MB/s     | 1300 MB/s  | 1260 MB/s  |


| Relative Perf  | Snappy size | Asm speed | Better size | Better Speed | Better vs. asm | Better dec |
|----------------|-------------|-----------|-------------|--------------|----------------|------------|
| html           | 22.31%      | 1.97x     | 13.03%      | 0.96x        | 0.49x          | 1.08x      |
| urls.10K       | 47.78%      | 1.53x     | 23.42%      | 0.87x        | 0.57x          | 0.92x      |
| fireworks.jpeg | 99.95%      | 0.96x     | -0.05%      | 0.38x        | 0.39x          | 1.11x      |
| paper-100k.pdf | 83.30%      | 2.40x     | 2.80%       | 0.12x        | 0.05x          | 0.61x      |
| html_x_4       | 22.52%      | 1.89x     | 78.45%      | 3.07x        | 1.62x          | 0.98x      |
| alice29.txt    | 57.88%      | 1.95x     | 16.59%      | 0.93x        | 0.48x          | 1.34x      |
| asyoulik.txt   | 61.91%      | 1.88x     | 13.56%      | 0.89x        | 0.47x          | 1.21x      |
| lcet10.txt     | 54.99%      | 1.86x     | 18.19%      | 0.93x        | 0.50x          | 1.19x      |
| plrabn12.txt   | 66.26%      | 1.78x     | 14.62%      | 0.90x        | 0.51x          | 1.15x      |
| geo.protodata  | 19.68%      | 2.10x     | 21.56%      | 1.10x        | 0.52x          | 1.18x      |
| kppkn.gtb      | 37.72%      | 2.04x     | 10.84%      | 0.93x        | 0.45x          | 0.97x      |

Except for the mostly incompressible JPEG image compression is better and usually in the double digits in terms of percentage reduction over Snappy.

The PDF sample shows a significant slowdown compared to Snappy, as this mode tries harder to compress the data.

# LICENSE

This code is based on the [Snappy-Go](https://github.com/golang/snappy) implementation.

Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.
