# lzw

This is a drop-in replacement for the standard library [`compress/lzw`](https://pkg.go.dev/compress/lzw) package,
with a **decompressor 1.4-4x faster** and a **compressor 1.1-2.7x faster**, depending on the data. See the
[measurements](#performance) for where in those ranges a given kind of input falls.

Both are exactly compatible: decoding produces byte-for-byte identical output and reports the same errors for
corrupt or truncated input, and encoding produces byte-for-byte identical compressed streams, so the
compression ratio is unchanged.

One difference to be aware of: `Read` writes codes out eight bytes at a time, so it may modify up to seven
bytes of the buffer you pass it beyond the count it returns. It never writes beyond the length of that
buffer, so `Read(buf[:k])` will not disturb `buf[k:]`.

* [Godoc documentation](https://pkg.go.dev/github.com/klauspost/compress/lzw)

# usage

Replace imports of `compress/lzw` with `github.com/klauspost/compress/lzw`. Nothing else has to change:
the package has the same API, including `Order`, `LSB`, `MSB`, `NewReader`, `NewWriter` and the `Reset`
methods.

Decompressing:

```Go
r := lzw.NewReader(src, lzw.LSB, 8)
defer r.Close()

if _, err := io.Copy(dst, r); err != nil {
    return err
}
```

Compressing:

```Go
w := lzw.NewWriter(dst, lzw.LSB, 8)
if _, err := w.Write(data); err != nil {
    return err
}
// Close flushes the final codes. It does not close dst.
return w.Close()
```

Use `LSB` with a literal width of 8 for GIF, and `MSB` for PDF. The literal width must be the same as the
one used when compressing.

# performance

Single core, AMD Ryzen 9 9950X. Decompression is measured decoding to a 64KB buffer.

Decompression:

| Input                        | `compress/lzw` | this package | speedup |
|------------------------------|---------------:|-------------:|--------:|
| `Mark.Twain-Tom.Sawyer.txt`  |      213 MB/s  |    821 MB/s  |  3.86x  |
| `html.txt`                   |      240 MB/s  |    818 MB/s  |  3.41x  |
| `e.txt` (digits)             |      307 MB/s  |    881 MB/s  |  2.87x  |
| `pngdata.bin` (binary)       |      558 MB/s  |   1842 MB/s  |  3.30x  |
| Tom Sawyer, `MSB` order      |      214 MB/s  |    844 MB/s  |  3.95x  |
| Tom Sawyer, litWidth 5       |      219 MB/s  |    875 MB/s  |  3.99x  |
| 1KB streams                  |      332 MB/s  |    559 MB/s  |  1.69x  |
| `sharnd.out` (random data)   |      238 MB/s  |    329 MB/s  |  1.38x  |

Compression:

| Input                        | `compress/lzw` | this package | speedup |
|------------------------------|---------------:|-------------:|--------:|
| `e.txt` (digits)             |      130 MB/s  |    356 MB/s  |  2.73x  |
| 1KB streams                  |      248 MB/s  |    478 MB/s  |  1.92x  |
| Tom Sawyer, litWidth 5       |      108 MB/s  |    194 MB/s  |  1.80x  |
| `html.txt`                   |      143 MB/s  |    230 MB/s  |  1.60x  |
| `sharnd.out` (random data)   |      160 MB/s  |    255 MB/s  |  1.60x  |
| `Mark.Twain-Tom.Sawyer.txt`  |      132 MB/s  |    194 MB/s  |  1.48x  |
| Tom Sawyer, `MSB` order      |      136 MB/s  |    191 MB/s  |  1.41x  |
| `pngdata.bin` (binary)       |      397 MB/s  |    431 MB/s  |  1.09x  |

Decoding gains most on the compressible data LZW is normally used for; incompressible input, which LZW
expands rather than compresses, gains the least since almost every code is a single literal byte. Encoding
gains most on input drawn from few distinct byte values, such as digits or a small colour palette, and least
on input made of long repeats, where it emits few codes to begin with.

Neither the reader nor the writer allocates while encoding or decoding, and reusing either with `Reset` does
not allocate at all — except that a source which is not an `io.ByteReader` is wrapped in a `bufio.Reader` on
each `Reset`, as it is in the standard library.

Run `go test -bench='Decode|Encode' -benchtime=500ms -count=10 .` in this directory to compare on your own
machine; `BenchmarkDecodeStd` and `BenchmarkEncodeStd` measure `compress/lzw` in the same process for a fair
comparison.

# getting the best speed

None of these are required for correct use, but each affects how fast the package runs.

**Decoding: read into a buffer of at least 8KB.** Given that much room the reader expands codes straight into
your buffer; with less it has to decode into an internal buffer and copy from it. `io.Copy` passes 32KB and so
needs no help, but note that a `bufio.Reader` wrapped *around* the reader defaults to 4KB.

**Decoding: prefer a source that can be read a machine word at a time.** The reader is careful never to
consume more of the source than the code stream needs, which is what makes it usable for embedded streams such
as the image data in a GIF. How it achieves that depends on the type passed to `NewReader`:

| Source                                                                                                   | How it is read                       |
|----------------------------------------------------------------------------------------------------------|--------------------------------------|
| any `io.Reader` that is not an `io.ByteReader`, e.g. `*os.File` or a network connection                  | wrapped in a `bufio.Reader`, fast    |
| `*bufio.Reader`, or anything with `Peek`/`Discard`/`Buffered`                                            | peeked, then discarded, fast         |
| `*bytes.Reader`, `*strings.Reader`, or any `io.ByteReader` that is also an `io.ReaderAt` and `io.Seeker` | read at an offset, then seeked, fast |
| anything else implementing `io.ByteReader`                                                               | one byte at a time, slower           |

The last row is the only slow case. It covers types such as `*bytes.Buffer`, and the block readers that
container formats often use. Wrapping such a source in a `bufio.Reader` is only safe if reading ahead of the
compressed stream is acceptable, since a `bufio.Reader` will consume whatever follows it.

**Either direction: reuse readers and writers with `Reset`.** Both hold their dictionary inline — a `Reader` is
around 56KB and a `Writer` around 68KB — so reusing one across streams, as when handling the frames of an
animated GIF, avoids repeated allocation. `Reset` on a `Reader` does not have to clear its tables, so it is
very cheap; on a `Writer` it clears the hash table, which is still far cheaper than allocating.

```Go
r := lzw.NewReader(frames[0], lzw.LSB, 8).(*lzw.Reader)
defer r.Close()

for _, frame := range frames {
    r.Reset(frame, lzw.LSB, 8)
    // ... read from r ...
}
```

## TIFF and PDF: the Aldus variant

TIFF uses a variant of LZW that increases the code width one code early — the "off by one" that Aldus
implemented and that libtiff keeps for compatibility. PDF's `LZWDecode` filter specifies the same variant by
default, through its `/EarlyChange` value of `1`. Decoding either needs one extra call:

```Go
r := lzw.NewReader(src, lzw.MSB, 8).(*lzw.Reader)
r.SetAldusCompatible(true)
defer r.Close()
```

Set it before the first `Read`. It is configuration rather than stream state, so it survives `Reset` and a
pooled reader keeps it.

The two variants are decoded by separate code, so leaving the setting off costs the default path nothing —
literally nothing on arm64, where `decode` compiles to the same bytes either way. The flip side is that the
Aldus path is the plain implementation rather than the tuned one: around 284 MB/s on Tom Sawyer where the
default path manages 848, still somewhat ahead of `compress/lzw`. Folding the variant into the tuned loop was
measured instead, and cost the default MSB path 7%, which was not a good trade for an opt-in.

Only the reader supports this so far — `Writer` always produces standard streams, which is what GIF wants and
what PDF accepts with `/EarlyChange 0`.

# testing

Both halves are tested by comparison with `compress/lzw` over every test input, both bit orders and all
literal widths.

Decoding must agree on the decoded bytes, on the error returned and on how many bytes of the source were
consumed, for each kind of source and a range of read sizes. Encoding must produce identical bytes, whatever
sizes the input is written in.

`FuzzReader` fuzzes the decoder comparison, including for corrupt and truncated input, `FuzzWriter` the
encoder comparison, and `FuzzRoundtrip` checks that what the writer produces decodes back to the input.
