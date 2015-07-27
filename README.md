# compress

This package is based on an optimized Deflate function, which is used by gzip/zip packages.

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

# gzip/zip optimizations
 * Uses the faster deflate
 * Uses SSE 4.2 CRC32 calculations.

Speed increase is up to 30%. Without SSE 4.1, speed is roughly equivalent, but compression shoudl be slightly better.

