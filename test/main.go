package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/klauspost/compress/zip"
)

func main() {
	fmt.Println("hello")

	f, err := os.Open("test/zipped.zip")
	if err != nil {
		log.Fatal(err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		log.Fatal(err)
	}
	fmt.Println("test/zipped.zip fi.Size()", fi.Size())
	f.Close()

	f, err = os.Open("test/zipped-end.zip")
	if err != nil {
		log.Fatal(err)
	}
	fi, err = f.Stat()
	if err != nil {
		f.Close()
		log.Fatal(err)
	}
	fmt.Println("test/zipped-end.zip fi.Size()", fi.Size())
	f.Close()

	////////////////////////////////////////////////////////////////////

	bs, err := ioutil.ReadFile("test/zipped.zip")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(bs))

	////////////////////////////////////////////////////////////////////

	// try manually calculating body offset

	// numbers grabbed manually from my logging
	header := bs[154:(154 + 30)] // header offset : header offset + fileHeaderLen
	v, err := findBodyOffset(header)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ELH: body offset", v)

	////////////////////////////////////////////////////////////////////

	// cut file and try to deflate manually

	// numbers grabbed manually from my logging
	cut := bs[253:(253 + 680)] // data offset : data offset + CompressedSize64

	outputFile, err := os.Create("test/zipped.decompressed")
	if err != nil {
		log.Fatal(err)
	}
	defer outputFile.Close()

	flateReader := flate.NewReader(bytes.NewReader(cut))

	defer flateReader.Close()
	io.Copy(outputFile, flateReader)

	////////////////////////////////////////////////////////////////////

	// I have learned that the file structs contain some useful context about file context
	// using only the calculations already in this package
	// baseOffset of the whole zip (even when reading from a tail-ed file): totalZipSize - tailedZipSize + (directoryEndOffset - int64(d.directorySize) - int64(d.directoryOffset))
	//
	// fileStart: headerOffset + bodyOffset
	// fileEnd: fileStart + CompressedSize64

	// expected flow:
	// fetch tail: grab at most, last 65k bytes (realistically, probably less but cannot be guaranteed)
	// use tail to find the  central repository which lists all files and their headerOffsets
	// fetch 30 bytes at a file's headerOffset to get the fileHeader. use this to derive bodyOffset
	// fetch CompressedSize64 bytes at a file's headerOffset + bodyOffset to get the compressed file contents
	// deflate compressed file contents

	////////////////////////////////////////////////////////////////////

	// r, err := zip.OpenReader("test/zipped-end.zip") // ^ still need to figure out offset. going to need to remove some negative index safe guards
	r, err := zip.OpenReader("test/zipped.zip")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	fmt.Println("len(r.File)", len(r.File))

	// Iterate through the files in the archive,
	// printing some of their contents.
	for _, f := range r.File {
		// lets just test 1 file for now
		if f.Name != "testfile.go" {
			continue
		}

		// you have to handle files
		if strings.HasSuffix(f.Name, "/") {
			fmt.Println("dir", f.Name)
			continue
		}
		fmt.Println("file", f.Name)

		fmt.Println("f.Method", f.Method)

		fmt.Println("f.HeaderOffset()", f.HeaderOffset())
		bodyOffset, err := f.FindBodyOffset()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("f.FindBodyOffset()", bodyOffset)
		fmt.Println("int64(f.CompressedSize64)", int64(f.CompressedSize64))

		dataOffset, err := f.DataOffset()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("f.DataOffset()", dataOffset)

		// example of reading contents
		// fmt.Printf("Contents of %s:\n", f.Name)
		// rc, err := f.Open()
		// if err != nil {
		// 	log.Fatal(err)
		// }
		// _, err = io.CopyN(os.Stdout, rc, 68)
		// if err != nil {
		// 	log.Fatal(err)
		// }
		// rc.Close()
		// fmt.Println()
	}
}

const fileHeaderLen int = 30
const fileHeaderSignature = 0x04034b50

var ErrFormat = errors.New("zip: not a valid zip file")

// modified to take in the 30 bytes of file header that we can get ourselves w/ file.HeaderOffset
func findBodyOffset(fileHeader []byte) (int64, error) {
	// var buf [fileHeaderLen]byte
	// if _, err := f.zipr.ReadAt(buf[:], headerOffset); err != nil {
	// 	return 0, err
	// }
	if len(fileHeader) != fileHeaderLen {
		return 0, errors.New("fileHeaderLen is not 30 bytes")
	}

	b := readBuf(fileHeader[:])
	if sig := b.uint32(); sig != fileHeaderSignature {
		return 0, ErrFormat
	}
	b = b[22:] // skip over most of the header
	filenameLen := int(b.uint16())
	extraLen := int(b.uint16())
	return int64(fileHeaderLen + filenameLen + extraLen), nil
}

/////////////////////////////////////////////////////////////////////////

type readBuf []byte

func (b *readBuf) uint8() uint8 {
	v := (*b)[0]
	*b = (*b)[1:]
	return v
}

func (b *readBuf) uint16() uint16 {
	v := binary.LittleEndian.Uint16(*b)
	*b = (*b)[2:]
	return v
}

func (b *readBuf) uint32() uint32 {
	v := binary.LittleEndian.Uint32(*b)
	*b = (*b)[4:]
	return v
}

func (b *readBuf) uint64() uint64 {
	v := binary.LittleEndian.Uint64(*b)
	*b = (*b)[8:]
	return v
}

func (b *readBuf) sub(n int) readBuf {
	b2 := (*b)[:n]
	*b = (*b)[n:]
	return b2
}
