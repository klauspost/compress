package zstd_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

func ExampleZipCompressor() {
	// Register zstandard compressors for zip.
	compr := zstd.ZipCompressor(zstd.WithWindowSize(1<<20), zstd.WithEncoderCRC(false))
	zip.RegisterCompressor(zstd.ZipMethodWinZip, compr)
	zip.RegisterCompressor(zstd.ZipMethodPKWare, compr)

	// Register zstandard decompressors for zip.
	decomp := zstd.ZipDecompressor()
	zip.RegisterDecompressor(zstd.ZipMethodWinZip, decomp)
	zip.RegisterDecompressor(zstd.ZipMethodPKWare, decomp)

	// Try it out...
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Create 1MB data
	tmp := make([]byte, 1<<20)
	for i := range tmp {
		tmp[i] = byte(i)
	}
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "file1.txt",
		Method: zstd.ZipMethodWinZip,
	})
	if err != nil {
		panic(err)
	}
	w.Write(tmp)

	// Another...
	w, err = zw.CreateHeader(&zip.FileHeader{
		Name:   "file2.txt",
		Method: zstd.ZipMethodPKWare,
	})
	w.Write(tmp)
	zw.Close()

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		panic(err)
	}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			panic(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if bytes.Equal(b, tmp) {
			fmt.Println(file.Name, "ok")
		} else {
			fmt.Println(file.Name, "mismatch")
		}
	}
	// Output:
	// file1.txt ok
	// file2.txt ok
}
