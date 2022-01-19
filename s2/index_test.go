package s2_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"sync"

	"github.com/klauspost/compress/s2"
)

func ExampleIndex_Load() {
	fatalErr := func(err error) {
		if err != nil {
			panic(err)
		}
	}

	// Create a test corpus
	tmp := make([]byte, 5<<20)
	rng := rand.New(rand.NewSource(0xbeefcafe))
	rng.Read(tmp)
	// Make it compressible...
	for i, v := range tmp {
		tmp[i] = '0' + v&3
	}
	// Compress it...
	var buf bytes.Buffer
	// We use smaller blocks just for the example...
	enc := s2.NewWriter(&buf, s2.WriterBlockSize(100<<10))
	err := enc.EncodeBuffer(tmp)
	fatalErr(err)

	// Close and get index...
	idxBytes, err := enc.CloseIndex()
	fatalErr(err)

	// This is our compressed stream...
	compressed := buf.Bytes()

	var once sync.Once
	for wantOffset := int64(0); wantOffset < int64(len(tmp)); wantOffset += 555555 {
		// Let's assume we want to read from uncompressed offset 'i'
		// and we cannot seek in input, but we have the index.
		want := tmp[wantOffset:]

		// Load the index.
		var index s2.Index
		_, err = index.Load(idxBytes)
		fatalErr(err)

		// Find offset in file:
		compressedOffset, uncompressedOffset, err := index.Find(wantOffset)
		fatalErr(err)

		// Offset the input to the compressed offset.
		// Notice how we do not provide any bytes before the offset.
		input := io.Reader(bytes.NewBuffer(compressed[compressedOffset:]))
		if _, ok := input.(io.Seeker); !ok {
			// Notice how the input cannot be seeked...
			once.Do(func() {
				fmt.Println("Input does not support seeking...")
			})
		} else {
			panic("did you implement seeking on bytes.Buffer?")
		}

		// When creating the decoder we must specify that it should not
		// expect a stream identifier at the beginning og the frame.
		dec := s2.NewReader(input, s2.ReaderIgnoreStreamIdentifier())

		// We now have a reader, but it will start outputting at uncompressedOffset,
		// and not the actual offset we want, so skip forward to that.
		toSkip := wantOffset - uncompressedOffset
		err = dec.Skip(toSkip)
		fatalErr(err)

		// Read the rest of the stream...
		got, err := ioutil.ReadAll(dec)
		fatalErr(err)
		if bytes.Equal(got, want) {
			fmt.Println("Successfully skipped forward to", wantOffset)
		} else {
			fmt.Println("Failed to skip forward to", wantOffset)
		}
	}
	// OUTPUT:
	//Input does not support seeking...
	//Successfully skipped forward to 0
	//Successfully skipped forward to 555555
	//Successfully skipped forward to 1111110
	//Successfully skipped forward to 1666665
	//Successfully skipped forward to 2222220
	//Successfully skipped forward to 2777775
	//Successfully skipped forward to 3333330
	//Successfully skipped forward to 3888885
	//Successfully skipped forward to 4444440
	//Successfully skipped forward to 4999995
}
