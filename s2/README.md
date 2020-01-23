# S2 Compression

S2 is an extension of [Snappy](https://github.com/google/snappy).

S2 is aimed for high throughput, which is why it features concurrent compression for bigger payloads.

Decoding is compatible with Snappy compressed content, but content compressed with S2 cannot be decompressed by Snappy.
This means that S2 can seamlessly replace Snappy without converting compressed content.

S2 is designed to have high throughput on content that cannot be compressed.
This is important so you don't have to worry about spending CPU cycles on already compressed data. 

## Benefits over Snappy

* Better compression
* Concurrent stream compression
* Faster decompression
* Ability to quickly skip forward in compressed stream
* Compatible with Snappy compressed content
* Offers alternative, more efficient, but slightly slower compression mode.
* Smaller block size overhead on incompressible blocks.
* Block concatenation
* Automatic stream size padding.

## Drawbacks over Snappy

* Not optimized for 32 bit systems.
* No AMD64 assembler implementation yet, meaning slightly slower compression speed on 1 core CPU.
* Uses slightly more memory due to larger blocks and concurrency (configurable).

# Usage

Installation: `go get -u github.com/klauspost/compress/s2`

Full package documentation:
 
[![godoc][1]][2]

[1]: https://godoc.org/github.com/klauspost/compress?status.svg
[2]: https://godoc.org/github.com/klauspost/compress/s2

Usage is similar to Snappy.

```Go
func EncodeStream(src io.Reader, dst io.Writer) error        
    enc := s2.NewWriter(dst)
    _, err := io.Copy(enc, src)
    if err != nil {
        enc.Close()
        return err
    }
    return enc.Close() 
}
```

You should always call `enc.Close()`, otherwise you will leak resources and your encode will be incomplete, as with Snappy.

For the best throughput, you should attempt to reuse the `Writer` using the `Reset()` method.

The Writer in S2 is always buffered, therefore `NewBufferedWriter` in Snappy can be replaced with `NewWriter` in S2.

```Go
func DecodeStream(src io.Reader, dst io.Writer) error        
    dec := s2.NewReader(src)
    _, err := io.Copy(dst, dec)
    return err
}
```

Similar to the Writer, a Reader can be reused using the `Reset` method.

For smaller data blocks, there is also a non-streaming interface: `Encode()`, `EncodeBetter()` and `Decode()`.
Do however note that these functions (similar to Snappy) does not provide validation of data, 
so data corruption may be undetected. Stream encoding provides CRC checks of data.

# Commandline tools

Some very simply commandline tools are provided; `s2c` for compression and `s2d` for decompression.

Binaries can be downloaded on the [Releases Page](https://github.com/klauspost/compress/releases).

Installing then requires Go to be installed. To install them, use:

`go install github.com/klauspost/compress/s2/cmd/s2c && go install github.com/klauspost/compress/s2/cmd/s2d`

To build binaries to the current folder use:

`go build github.com/klauspost/compress/s2/cmd/s2c && go build github.com/klauspost/compress/s2/cmd/s2d`


## s2c

```
Usage: s2c [options] file1 file2

Compresses all files supplied as input separately.
Output files are written as 'filename.ext.s2'.
By default output files will be overwritten.
Use - as the only file name to read from stdin and write to stdout.

Wildcards are accepted: testdir/*.txt will compress all files in testdir ending with .txt
Directories can be wildcards as well. testdir/*/*.txt will match testdir/subdir/b.txt

Options:
  -bench int
    	Run benchmark n times. No output will be written
  -blocksize string
    	Max  block size. Examples: 64K, 256K, 1M, 4M. Must be power of two and <= 4MB (default "4M")
  -c	Write all output to stdout. Multiple input files will be concatenated
  -cpu int
    	Compress using this amount of threads (default CPU_THREADS])
  -faster
    	Compress faster, but with a minor compression loss
  -help
    	Display help
  -pad string
    	Pad size to a multiple of this value, Examples: 500, 64K, 256K, 1M, 4M, etc (default "1")
  -q	Don't write any output to terminal, except errors
  -rm
    	Delete source file(s) after successful compression
  -safe
    	Do not overwrite output files
```

## s2d

```
Usage: s2d [options] file1 file2

Decompresses all files supplied as input. Input files must end with '.s2' or '.snappy'.
Output file names have the extension removed. By default output files will be overwritten.
Use - as the only file name to read from stdin and write to stdout.

Wildcards are accepted: testdir/*.txt will compress all files in testdir ending with .txt
Directories can be wildcards as well. testdir/*/*.txt will match testdir/subdir/b.txt

Options:
  -bench int
    	Run benchmark n times. No output will be written
  -c	Write all output to stdout. Multiple input files will be concatenated
  -help
    	Display help
  -q	Don't write any output to terminal, except errors
  -rm
    	Delete source file(s) after successful decompression
  -safe
    	Do not overwrite output files

```

# Performance

This section will focus on comparisons to Snappy. 
This package is solely aimed at replacing Snappy as a high speed compression package.
If you are mainly looking for better compression [zstandard](https://github.com/klauspost/compress/tree/master/zstd#zstd)
gives better compression, but typically at speeds slightly below "better" mode in this package.

Compression is increased compared to Snappy, mostly around 5-20% and the throughput is typically 25-40% increased (single threaded) compared to the non-assembly Go implementation.

A "better" compression mode is also available. This allows to trade a bit of speed for a minor compression gain.
The content compressed in this mode is fully compatible with the standard decoder.

Snappy vs S2 compression speed on 6 core (12 thread) computer, using all threads and a single thread:

| File                                                                                                | S2 speed | S2 throughput | S2 % smaller | S2 decomp | S2 "better" | "better" throughput | "better" % smaller | "better" decomp |
|-----------------------------------------------------------------------------------------------------|----------|---------------|--------------|-----------|-------------|---------------------|--------------------|-----------------|
| [rawstudio-mint14.tar](https://files.klauspost.com/compress/rawstudio-mint14.7z)                    | 7.41x    | 3401 MB/s     | 6.98%        | 1.19x     | 3.73x       | 1713 MB/s           | 10.97%             | 0.96x           |
| (1 CPU)                                                                                             | 1.37x    | 631 MB/s      | -            | 2085 MB/s | 0.67x       | 309 MB/s            | -                  | 1691 MB/s       |
| [github-june-2days-2019.json](https://files.klauspost.com/compress/github-june-2days-2019.json.zst) | 8.76x    | 4351 MB/s     | 28.79%       | 1.29x     | 6.64x       | 3301 MB/s           | 32.43%             | 1.23x           |
| (1 CPU)                                                                                             | 1.62x    | 806 MB/s      | -            | 2262 MB/s | 1.08x       | 535 MB/s            | -                  | 2153 MB/s       |
| [github-ranks-backup.bin](https://files.klauspost.com/compress/github-ranks-backup.bin.zst)         | 7.70x    | 3610 MB/s     | -5.90%       | 1.07x     | 4.65x       | 2179 MB/s           | 5.45%              | 0.93x           |
| (1 CPU)                                                                                             | 1.26x    | 592 MB/s      | -            | 2053 MB/s | 0.76x       | 356 MB/s            | -                  | 1796 MB/s       |
| [consensus.db.10gb](https://files.klauspost.com/compress/consensus.db.10gb.zst)                     | 7.17x    | 3494 MB/s     | 14.83%       | 1.03x     | 3.43x       | 1674 MB/s           | 14.79%             | 1.03x           |
| (1 CPU)                                                                                             | 1.41x    | 687 MB/s      | -            | 2805 MB/s | 0.63x       | 309 MB/s            | -                  | 2796 MB/s       |
| [adresser.json](https://files.klauspost.com/compress/adresser.json.zst)                             | 5.16x    | 4923 MB/s     | 43.52%       | 1.17x     | 4.67x       | 4456 MB/s           | 47.15%             | 1.19x           |
| (1 CPU)                                                                                             | 1.76x    | 1675 MB/s     | -            | 3985 MB/s | 1.49x       | 1425 MB/s           | -                  | 4034 MB/s       |
| [gob-stream](https://files.klauspost.com/compress/gob-stream.7z)                                    | 8.84x    | 4402 MB/s     | 22.24%       | 1.16x     | 6.58x       | 3278 MB/s           | 25.91%             | 1.08x           |
| (1 CPU)                                                                                             | 1.50x    | 747 MB/s      | -            | 2175 MB/s | 1.06x       | 530 MB/s            | -                  | 2039 MB/s       |
| [10gb.tar](http://mattmahoney.net/dc/10gb.html)                                                     | 6.73x    | 2715 MB/s     | 1.99%        | 1.04x     | 4.50x       | 1818 MB/s           | 5.68%              | 0.91x           |
| (1 CPU)                                                                                             | 1.17x    | 472 MB/s      | -            | 1493 MB/s | 0.79x       | 320 MB/s            | -                  | 1312 MB/s       |
| sharnd.out.2gb                                                                                      | 0.94x    | 5987 MB/s     | 0.01%        | 0.89x     | 0.90x       | 5768 MB/s           | 0.01%              | 0.90x           |
| (1 CPU)                                                                                             | 1.30x    | 8323 MB/s     | -            | 4222 MB/s | 1.18x       | 7528 MB/s           | -                  | 4266 MB/s       |
| [enwik9](http://mattmahoney.net/dc/textdata.html)                                                   | 10.02x   | 2337 MB/s     | 3.66%        | 1.35x     | 5.83x       | 1360 MB/s           | 15.37%             | 1.05x           |
| (1 CPU)                                                                                             | 1.38x    | 321 MB/s      | -            | 1230 MB/s | 0.84x       | 197 MB/s            | -                  | 956 MB/s        |
| [silesia.tar](http://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip)                                    | 7.72x    | 2464 MB/s     | 5.63%        | 1.22x     | 4.65x       | 1486 MB/s           | 12.42%             | 1.01x           |
| (1 CPU)                                                                                             | 1.31x    | 418 MB/s      | -            | 1454 MB/s | 0.77x       | 246 MB/s            | -                  | 1210 MB/s       |
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
S2 prefers longer matches and will typically only find matches that are 6 bytes or longer. 
While this reduces compression a bit, it improves decompression speed.

The "better" compression mode will actively look for shorter matches, which is why it has a decompression speed quite similar to Snappy.   

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

Block compression. Parallel benchmark running on 16 cores, 16 goroutines.

AMD64 assembly is use for both S2 and Snappy.

| Absolute Perf         | Snappy size | S2 Size | Snappy Speed | S2 Speed    | Snappy dec  | S2 dec      |
|-----------------------|-------------|---------|--------------|-------------|-------------|-------------|
| html                  | 22843       | 21111   | 16246 MB/s   | 17438 MB/s  | 40972 MB/s  | 49263 MB/s  |
| urls.10K              | 335492      | 287326  | 7943 MB/s    | 9693 MB/s   | 22523 MB/s  | 26484 MB/s  |
| fireworks.jpeg        | 123034      | 123100  | 349544 MB/s  | 266024 MB/s | 718321 MB/s | 827552 MB/s |
| fireworks.jpeg (200B) | 146         | 155     | 8869 MB/s    | 19730 MB/s  | 33691 MB/s  | 52421 MB/s  |
| paper-100k.pdf        | 85304       | 84459   | 167546 MB/s  | 101263 MB/s | 326905 MB/s | 291944 MB/s |
| html_x_4              | 92234       | 21113   | 15194 MB/s   | 50670 MB/s  | 30843 MB/s  | 32217 MB/s  |
| alice29.txt           | 88034       | 85975   | 5936 MB/s    | 6139 MB/s   | 12882 MB/s  | 20044 MB/s  |
| asyoulik.txt          | 77503       | 79650   | 5517 MB/s    | 6366 MB/s   | 12735 MB/s  | 22806 MB/s  |
| lcet10.txt            | 234661      | 220670  | 6235 MB/s    | 6067 MB/s   | 14519 MB/s  | 18697 MB/s  |
| plrabn12.txt          | 319267      | 317985  | 5159 MB/s    | 5726 MB/s   | 11923 MB/s  | 19901 MB/s  |
| geo.protodata         | 23335       | 18690   | 21220 MB/s   | 26529 MB/s  | 56271 MB/s  | 62540 MB/s  |
| kppkn.gtb             | 69526       | 65312   | 9732 MB/s    | 8559 MB/s   | 18491 MB/s  | 18969 MB/s  |
| alice29.txt (128B)    | 80          | 84      | 6691 MB/s    | 15542 MB/s  | 31883 MB/s  | 37851 MB/s  |
| alice29.txt (1000B)   | 774         | 852     | 12204 MB/s   | 21176 MB/s  | 48056 MB/s  | 100995 MB/s |
| alice29.txt (10000B)  | 6648        | 7437    | 10044 MB/s   | 13550 MB/s  | 32378 MB/s  | 52489 MB/s  |
| alice29.txt (20000B)  | 12686       | 13574   | 7733 MB/s    | 11210 MB/s  | 30566 MB/s  | 48503 MB/s  |


| Relative Perf         | Snappy size | S2 size improved | S2 Speed | S2 Dec Speed |
|-----------------------|-------------|------------------|----------|--------------|
| html                  | 22.31%      | 7.58%            | 1.07x    | 1.20x        |
| urls.10K              | 47.78%      | 14.36%           | 1.22x    | 1.18x        |
| fireworks.jpeg        | 99.95%      | -0.05%           | 0.76x    | 1.15x        |
| fireworks.jpeg (200B) | 73.00%      | -6.16%           | 2.22x    | 1.56x        |
| paper-100k.pdf        | 83.30%      | 0.99%            | 0.60x    | 0.89x        |
| html_x_4              | 22.52%      | 77.11%           | 3.33x    | 1.04x        |
| alice29.txt           | 57.88%      | 2.34%            | 1.03x    | 1.56x        |
| asyoulik.txt          | 61.91%      | -2.77%           | 1.15x    | 1.79x        |
| lcet10.txt            | 54.99%      | 5.96%            | 0.97x    | 1.29x        |
| plrabn12.txt          | 66.26%      | 0.40%            | 1.11x    | 1.67x        |
| geo.protodata         | 19.68%      | 19.91%           | 1.25x    | 1.11x        |
| kppkn.gtb             | 37.72%      | 6.06%            | 0.88x    | 1.03x        |
| alice29.txt (128B)    | 62.50%      | -5.00%           | 2.32x    | 1.19x        |
| alice29.txt (1000B)   | 77.40%      | -10.08%          | 1.74x    | 2.10x        |
| alice29.txt (10000B)  | 66.48%      | -11.87%          | 1.35x    | 1.62x        |
| alice29.txt (20000B)  | 63.43%      | -7.00%           | 1.45x    | 1.59x        |

Speed is generally at or above Snappy. Small blocks gets a significant speedup, although at the expense of size. 

Decompression speed is better than Snappy, except in one case. 

Since payloads are very small the variance in terms of size is rather big, so they should only be seen as a general guideline.

Size is on average around Snappy, but varies on content type. 
In cases where compression is worse, it usually is compensated by a speed boost. 


### Better compression

| Absolute Perf         | Snappy size | Better Size | Snappy Speed | Better Speed | Snappy dec  | Better dec  |
|-----------------------|-------------|-------------|--------------|--------------|-------------|-------------|
| html                  | 22843       | 19833       | 16246 MB/s   | 7731 MB/s    | 40972 MB/s  | 40292 MB/s  |
| urls.10K              | 335492      | 253529      | 7943 MB/s    | 3980 MB/s    | 22523 MB/s  | 20981 MB/s  |
| fireworks.jpeg        | 123034      | 123100      | 349544 MB/s  | 9760 MB/s    | 718321 MB/s | 823698 MB/s |
| fireworks.jpeg (200B) | 146         | 142         | 8869 MB/s    | 594 MB/s     | 33691 MB/s  | 30101 MB/s  |
| paper-100k.pdf        | 85304       | 82915       | 167546 MB/s  | 7470 MB/s    | 326905 MB/s | 198869 MB/s |
| html_x_4              | 92234       | 19841       | 15194 MB/s   | 23403 MB/s   | 30843 MB/s  | 30937 MB/s  |
| alice29.txt           | 88034       | 73218       | 5936 MB/s    | 2945 MB/s    | 12882 MB/s  | 16611 MB/s  |
| asyoulik.txt          | 77503       | 66844       | 5517 MB/s    | 2739 MB/s    | 12735 MB/s  | 14975 MB/s  |
| lcet10.txt            | 234661      | 190589      | 6235 MB/s    | 3099 MB/s    | 14519 MB/s  | 16634 MB/s  |
| plrabn12.txt          | 319267      | 270828      | 5159 MB/s    | 2600 MB/s    | 11923 MB/s  | 13382 MB/s  |
| geo.protodata         | 23335       | 18278       | 21220 MB/s   | 11208 MB/s   | 56271 MB/s  | 57961 MB/s  |
| kppkn.gtb             | 69526       | 61851       | 9732 MB/s    | 4556 MB/s    | 18491 MB/s  | 16524 MB/s  |
| alice29.txt (128B)    | 80          | 81          | 6691 MB/s    | 529 MB/s     | 31883 MB/s  | 34225 MB/s  |
| alice29.txt (1000B)   | 774         | 748         | 12204 MB/s   | 1943 MB/s    | 48056 MB/s  | 42068 MB/s  |
| alice29.txt (10000B)  | 6648        | 6234        | 10044 MB/s   | 2949 MB/s    | 32378 MB/s  | 28813 MB/s  |
| alice29.txt (20000B)  | 12686       | 11584       | 7733 MB/s    | 2822 MB/s    | 30566 MB/s  | 27315 MB/s  |


| Relative Perf         | Snappy size | Better size | Better Speed | Better dec |
|-----------------------|-------------|-------------|--------------|------------|
| html                  | 22.31%      | 13.18%      | 0.48x        | 0.98x      |
| urls.10K              | 47.78%      | 24.43%      | 0.50x        | 0.93x      |
| fireworks.jpeg        | 99.95%      | -0.05%      | 0.03x        | 1.15x      |
| fireworks.jpeg (200B) | 73.00%      | 2.74%       | 0.07x        | 0.89x      |
| paper-100k.pdf        | 83.30%      | 2.80%       | 0.07x        | 0.61x      |
| html_x_4              | 22.52%      | 78.49%      | 0.04x        | 1.00x      |
| alice29.txt           | 57.88%      | 16.83%      | 1.54x        | 1.29x      |
| asyoulik.txt          | 61.91%      | 13.75%      | 0.50x        | 1.18x      |
| lcet10.txt            | 54.99%      | 18.78%      | 0.50x        | 1.15x      |
| plrabn12.txt          | 66.26%      | 15.17%      | 0.50x        | 1.12x      |
| geo.protodata         | 19.68%      | 21.67%      | 0.50x        | 1.03x      |
| kppkn.gtb             | 37.72%      | 11.04%      | 0.53x        | 0.89x      |
| alice29.txt (128B)    | 62.50%      | -1.25%      | 0.47x        | 1.07x      |
| alice29.txt (1000B)   | 77.40%      | 3.36%       | 0.08x        | 0.88x      |
| alice29.txt (10000B)  | 66.48%      | 6.23%       | 0.16x        | 0.89x      |
| alice29.txt (20000B)  | 63.43%      | 8.69%       | 0.29x        | 0.89x      |

Except for the mostly incompressible JPEG image compression is better and usually in the 
double digits in terms of percentage reduction over Snappy.

The PDF sample shows a significant slowdown compared to Snappy, as this mode tries harder 
to compress the data.

This mode aims to provide better compression at the expense of performance and achieves that 
without a huge performance pentalty, except on very small blocks. 

Decompression speed suffers a little compared to the regular S2 mode, 
but still manages to be close to Snappy in spite of increased compression.  
 
# Concatenating blocks and streams.

Concatenating streams will concatenate the output of both without recompressing them. 
While this is inefficient in terms of compression it might be usable in certain scenarios. 
The 10 byte 'stream identifier' of the second stream can optionally be stripped, but it is not a requirement.

Blocks can be concatenated using the `ConcatBlocks` function.

Snappy blocks/streams can safely be concatenated with S2 blocks and streams. 

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

The first copy of a block cannot be a repeat offset and the offset is not carried across blocks in streams.

Default streaming block size is 1MB.

# LICENSE

This code is based on the [Snappy-Go](https://github.com/golang/snappy) implementation.

Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.
