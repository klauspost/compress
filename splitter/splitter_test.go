package splitter

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"testing"
)

// Returns a deterministic buffer of size n
func getBufferSize(n int) *bytes.Buffer {
	rand.Seed(0)
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.Intn(255))
	}
	return bytes.NewBuffer(b)
}

func TestFixedFragment(t *testing.T) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 64 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	out := make(chan []byte, 10)
	count := make(chan int, 0)
	go func() {
		n := 0
		for f := range out {
			if !bytes.Equal(b[n:n+len(f)], f) {
				panic(fmt.Sprintf("output mismatch at offset %d", n))
			}
			n += len(f)
		}
		count <- n
	}()
	input = bytes.NewBuffer(b)
	w, err := New(out, ModeFixed, size)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(w, input)
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	datalen := <-count

	t.Log("Data size:", datalen)
	if datalen != totalinput {
		t.Fatalf("got %d bytes, want %d", datalen, totalinput)
	}
}

func TestPredictionFragment(t *testing.T) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 64 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	out := make(chan []byte, 10)
	count := make(chan int, 0)
	go func() {
		n := 0
		for f := range out {
			if !bytes.Equal(b[n:n+len(f)], f) {
				panic(fmt.Sprintf("output mismatch at offset %d", n))
			}
			n += len(f)
		}
		count <- n
	}()
	input = bytes.NewBuffer(b)
	w, err := New(out, ModePrediction, size)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(w, input)
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	datalen := <-count

	t.Log("Data size:", datalen)
	if datalen != totalinput {
		t.Fatalf("got %d bytes, want %d", datalen, totalinput)
	}
}

func TestEntropyFragment(t *testing.T) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 64 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	out := make(chan []byte, 10)
	count := make(chan int, 0)
	go func() {
		n := 0
		for f := range out {
			if !bytes.Equal(b[n:n+len(f)], f) {
				panic(fmt.Sprintf("output mismatch at offset %d", n))
			}
			n += len(f)
		}
		count <- n
	}()
	input = bytes.NewBuffer(b)
	w, err := New(out, ModeEntropy, size)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(w, input)
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	datalen := <-count

	t.Log("Data size:", datalen)
	if datalen != totalinput {
		t.Fatalf("got %d bytes, want %d", datalen, totalinput)
	}
}

// Maximum block size:64k
func BenchmarkDynamicFragments64K(t *testing.B) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 64 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	t.ResetTimer()
	t.SetBytes(totalinput)
	for i := 0; i < t.N; i++ {
		out := make(chan []byte, 10)
		go func() {
			for range out {
			}
		}()
		input = bytes.NewBuffer(b)
		w, _ := New(out, ModePrediction, size)
		io.Copy(w, input)
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Maximum block size:256k
func BenchmarkDynamicEntropyFragments256K(t *testing.B) {
	const totalinput = 40 << 20
	input := getBufferSize(totalinput)

	const size = 256 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	t.ResetTimer()
	t.SetBytes(totalinput)
	for i := 0; i < t.N; i++ {
		out := make(chan []byte, 10)
		go func() {
			for range out {
			}
		}()
		input = bytes.NewBuffer(b)
		w, _ := New(out, ModeEntropy, size)
		io.Copy(w, input)
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Maximum block size:64k
func BenchmarkDynamicEntropyFragments64K(t *testing.B) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 64 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	t.ResetTimer()
	t.SetBytes(totalinput)
	for i := 0; i < t.N; i++ {
		out := make(chan []byte, 10)
		go func() {
			for range out {
			}
		}()
		input = bytes.NewBuffer(b)
		w, _ := New(out, ModeEntropy, size)
		io.Copy(w, input)
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Maximum block size:4k
func BenchmarkDynamicEntropyFragments4K(t *testing.B) {
	const totalinput = 10 << 20
	input := getBufferSize(totalinput)

	const size = 4 << 10
	b := input.Bytes()
	// Create some duplicates
	for i := 0; i < 50; i++ {
		// Read from 10 first blocks
		src := b[(i%10)*size : (i%10)*size+size]
		// Write into the following ones
		dst := b[(10+i)*size : (i+10)*size+size]
		copy(dst, src)
	}
	t.ResetTimer()
	t.SetBytes(totalinput)
	for i := 0; i < t.N; i++ {
		out := make(chan []byte, 10)
		go func() {
			for range out {
			}
		}()
		input = bytes.NewBuffer(b)
		w, _ := New(out, ModeEntropy, size)
		io.Copy(w, input)
		err := w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
