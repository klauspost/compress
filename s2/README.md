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

| File                 | S2 Througput | S2 % smaller |
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

# LICENSE

This code is based on the [Snappy-Go](https://github.com/golang/snappy) implementation.

Use of this source code is governed by a BSD-style license that can be found in the LICENSE file.
