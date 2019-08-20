# S2 Compression

S2 is an extension of [Snappy](https://github.com/google/snappy).

Decoding is compatible with Snappy compressed content, but content compressed with S2 cannot be decompressed by Snappy.

This means that S2 can seamlessly replace Snappy without converting compressed content.

# Extensions

* Frame [Stream identifier](https://github.com/google/snappy/blob/master/framing_format.txt#L68) changed from `sNaPpY` to `S2sTwO`.
* [Framed compressed blocks](https://github.com/google/snappy/blob/master/format_description.txt) can be up to 1MB (up from 64KB).
* Compressed blocks can have an offset of `0`, which indicates to repeat the last offset.

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


# Performance

Decompression remains close to original Snappy speed, with a single additional branch for 1 byte offset matches. So only minor differences should be assumed there.
Only if your decompression platform is heavily memory limited, will there be a difference.

Compression is increased, mostly around 5-20% and the throughput is typically 25-40% increased compared to the non-assembly Go implementation.

| File                 | S2 throughput | S2 % smaller |
|----------------------|--------------|-----------------|
| [consensus.db.10gb](https://files.klauspost.com/compress/consensus.db.10gb.zst)    | 1.48x        | 14.83%          |
| [enwik9](http://mattmahoney.net/dc/textdata.html)               | 1.41x        | 2.79%           |
| [gob-stream](https://files.klauspost.com/compress/gob-stream.7z)           | 1.68x        | 23.08%          |
| [adresser.json](https://files.klauspost.com/compress/adresser.json.zst)        | 2.18x        | 45.58%          |
| [rawstudio-mint14.tar](https://files.klauspost.com/compress/rawstudio-mint14.7z) | 1.41x        | 5.67%           |
| [10gb.tar](http://mattmahoney.net/dc/10gb.html)             | 1.24x        | 0.10%          |
| [silesia.tar](http://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip)          | 1.38x        | 3.71%           |

There is a good speedup across the board.

Machine generated data gets by far the biggest compression boost, with size being being reduced by up to 45% of Snappy size.

It would be very feasible to add faster/better compression modes to S2, but the current settings are a good replacement for Snappy.

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

This is the single goroutine decompression speed:

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

Even though S2 typically compresses better than Snappy, decompression speed is always better. 

# LICENSE

This code is based on the [Snappy-Go](https://github.com/golang/snappy) implementation.

Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.
