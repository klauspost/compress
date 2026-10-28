// Copyright (c) 2021 Klaus Post. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gzhttp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

func TestTransport(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(newTestHandler(bin))

	c := http.Client{Transport: Transport(http.DefaultTransport)}
	resp, err := c.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
}

func TestTransportReuseRequest(t *testing.T) {
	payload := bytes.Repeat([]byte("reusable request body"), 100)
	for _, useZstd := range []bool{false, true} {
		name := "gzip"
		if useZstd {
			name = "zstd"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(newTestHandler(payload))
			defer server.Close()
			client := http.Client{Transport: Transport(http.DefaultTransport, TransportEnableZstd(useZstd))}
			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Custom", "preserved")
			for attempt := range 2 {
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("attempt %d: read error %v, close error %v", attempt, readErr, closeErr)
				}
				if !bytes.Equal(body, payload) {
					t.Errorf("attempt %d: response was not decompressed", attempt)
				}
				if !resp.Uncompressed {
					t.Errorf("attempt %d: response is not marked Uncompressed", attempt)
				}
				if got := req.Header.Get("Accept-Encoding"); got != "" {
					t.Errorf("attempt %d: request Accept-Encoding changed to %q", attempt, got)
				}
				if got := req.Header.Get("X-Custom"); got != "preserved" {
					t.Errorf("attempt %d: request X-Custom changed to %q", attempt, got)
				}
			}
		})
	}
}

func TestTransportForced(t *testing.T) {
	raw, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	bin := buf.Bytes()

	server := httptest.NewServer(newTestHandler(bin))

	c := http.Client{Transport: Transport(http.DefaultTransport)}
	resp, err := c.Get(server.URL + "/gzipped")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("data mismatch")
	}
}

func TestTransportForcedDisabled(t *testing.T) {
	raw, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	bin := buf.Bytes()

	server := httptest.NewServer(newTestHandler(bin))
	c := http.Client{Transport: Transport(http.DefaultTransport, TransportEnableGzip(false))}
	resp, err := c.Get(server.URL + "/gzipped")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bin, got) {
		t.Errorf("data mismatch")
	}
}

func TestTransportZstd(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := zstd.NewWriter(nil)
	defer enc.Close()
	zsBin := enc.EncodeAll(bin, nil)
	server := httptest.NewServer(newTestHandler(zsBin))

	c := http.Client{Transport: Transport(http.DefaultTransport)}
	resp, err := c.Get(server.URL + "/zstd")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
}

func TestTransportInvalid(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(newTestHandler(bin))

	c := http.Client{Transport: Transport(http.DefaultTransport)}
	// Serves json as gzipped...
	resp, err := c.Get(server.URL + "/gzipped")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTransportZstdDisabled(t *testing.T) {
	raw, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	enc, _ := zstd.NewWriter(nil)
	defer enc.Close()
	zsBin := enc.EncodeAll(raw, nil)

	server := httptest.NewServer(newTestHandler(zsBin))
	c := http.Client{Transport: Transport(http.DefaultTransport, TransportEnableZstd(false))}
	resp, err := c.Get(server.URL + "/zstd")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zsBin, got) {
		t.Errorf("data mismatch")
	}
}

func TestTransportZstdInvalid(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}
	// Do not encode...
	server := httptest.NewServer(newTestHandler(bin))

	c := http.Client{Transport: Transport(http.DefaultTransport)}
	resp, err := c.Get(server.URL + "/zstd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	t.Log("expected error:", err)
}

func TestDefaultTransport(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(newTestHandler(bin))

	// Not wrapped...
	c := http.Client{Transport: http.DefaultTransport}
	resp, err := c.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
}

func TestTransportCustomEval(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(newTestHandler(bin))
	calledWith := ""
	c := http.Client{Transport: Transport(http.DefaultTransport, TransportEnableZstd(false), TransportCustomEval(func(h http.Header) bool {
		calledWith = h.Get("Content-Encoding")
		return true
	}))}
	resp, err := c.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
	if calledWith != "gzip" {
		t.Fatalf("Expected encoding %q, got %q", "gzip", calledWith)
	}
	// Test returning false - now with zstd enabled on client, server prefers zstd
	c = http.Client{Transport: Transport(http.DefaultTransport, TransportCustomEval(func(h http.Header) bool {
		calledWith = h.Get("Content-Encoding")
		return false
	}))}
	resp, err = c.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Check we got the compressed data (zstd is now preferred by default)
	gotCE := resp.Header.Get("Content-Encoding")
	if gotCE != "zstd" {
		t.Fatalf("Expected encoding %q, got %q", "zstd", gotCE)
	}
	if calledWith != "zstd" {
		t.Fatalf("Expected encoding %q, got %q", "zstd", calledWith)
	}
}

func TestTransportTransportAlwaysDecompress(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}

	// We will serve the data as zstd+gzip, but the client will not request it.
	server := httptest.NewServer(newTestHandler(bin))
	c := http.Client{Transport: Transport(http.DefaultTransport, TransportEnableZstd(false), TransportEnableGzip(false), TransportAlwaysDecompress(true))}
	resp, err := c.Get(server.URL + "/zstd/do")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
	resp.Body.Close()

	resp, err = c.Get(server.URL + "/gzip/do")
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data mismatch")
	}
	resp.Body.Close()

	// We will serve the data as zstd+gzip, but the client will not request it.
	// With TransportAlwaysDecompress(false) it should not be decompressed.
	c = http.Client{Transport: Transport(http.DefaultTransport, TransportEnableZstd(false), TransportEnableGzip(false), TransportAlwaysDecompress(false))}
	resp, err = c.Get(server.URL + "/zstd/do")
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, bin) {
		t.Errorf("data matches")
	}
	resp.Body.Close()

	resp, err = c.Get(server.URL + "/gzip/do")
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bin) {
		t.Errorf("data matches")
	}
	resp.Body.Close()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// stallReader blocks in Read until release is closed, then returns err.
type stallReader struct {
	stalled, release chan struct{}
	err              error
}

func newStallReader(err error) *stallReader {
	return &stallReader{stalled: make(chan struct{}), release: make(chan struct{}), err: err}
}

func (s *stallReader) Read([]byte) (int, error) {
	select {
	case <-s.stalled:
	default:
		close(s.stalled)
	}
	<-s.release
	return 0, s.err
}

// TestTransportCloseDuringRead closes a response body while a Read is blocked
// on it, as net/http allows, and checks that the decoder that Read is using
// is not handed to the next response before the Read returns.
func TestTransportCloseDuringRead(t *testing.T) {
	bin, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		t.Fatal(err)
	}
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	gz.Write(bin)
	gz.Close()
	enc, _ := zstd.NewWriter(nil)
	defer enc.Close()

	for _, test := range []struct {
		encoding string
		data     []byte
	}{
		{encoding: "gzip", data: gzBuf.Bytes()},
		{encoding: "zstd", data: enc.EncodeAll(bin, nil)},
	} {
		t.Run(test.encoding, func(t *testing.T) {
			half := len(test.data) / 2
			// The first body fails once released, like a closed connection.
			// The second stalls half-way, then completes.
			stallA, stallB := newStallReader(io.ErrClosedPipe), newStallReader(io.EOF)
			bodies := []io.Reader{
				io.MultiReader(bytes.NewReader(test.data[:half]), stallA),
				io.MultiReader(bytes.NewReader(test.data[:half]), stallB, bytes.NewReader(test.data[half:])),
			}
			tr := Transport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := bodies[0]
				bodies = bodies[1:]
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Encoding": {test.encoding}},
					Body:       io.NopCloser(body),
				}, nil
			}))
			get := func() (io.ReadCloser, chan []byte) {
				req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
				resp, err := tr.RoundTrip(req)
				if err != nil {
					t.Fatal(err)
				}
				got := make(chan []byte, 1)
				go func() {
					b, _ := io.ReadAll(resp.Body)
					got <- b
				}()
				return resp.Body, got
			}

			bodyA, gotA := get()
			<-stallA.stalled
			bodyA.Close()
			bodyB, gotB := get()
			<-stallB.stalled
			// Let the first Read return while the second response is mid-stream.
			close(stallA.release)
			<-gotA
			close(stallB.release)
			if got := <-gotB; !bytes.Equal(got, bin) {
				t.Errorf("second response: got %d bytes, want %d", len(got), len(bin))
			}
			bodyB.Close()
		})
	}
}

func BenchmarkTransport(b *testing.B) {
	raw, err := os.ReadFile("testdata/benchmark.json")
	if err != nil {
		b.Fatal(err)
	}
	sz := int64(len(raw))
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	bin := buf.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(bin)
	}))
	enc, _ := zstd.NewWriter(nil, zstd.WithWindowSize(128<<10), zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	defer enc.Close()
	zsBin := enc.EncodeAll(raw, nil)
	serverZstd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body.Close()
		w.Header().Set("Content-Encoding", "zstd")
		w.WriteHeader(http.StatusOK)
		w.Write(zsBin)
	}))
	b.Run("gzhttp", func(b *testing.B) {
		c := http.Client{Transport: Transport(http.DefaultTransport)}

		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := c.Get(server.URL + "/gzipped")
			if err != nil {
				b.Fatal(err)
			}
			n, err := io.Copy(io.Discard, resp.Body)
			if err != nil {
				b.Fatal(err)
			}
			if n != sz {
				b.Fatalf("size mismatch: want %d, got %d", sz, n)
			}
			resp.Body.Close()
		}
		b.ReportMetric(100*float64(len(bin))/float64(len(raw)), "pct")
	})
	b.Run("stdlib", func(b *testing.B) {
		c := http.Client{Transport: http.DefaultTransport}
		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := c.Get(server.URL + "/gzipped")
			if err != nil {
				b.Fatal(err)
			}
			n, err := io.Copy(io.Discard, resp.Body)
			if err != nil {
				b.Fatal(err)
			}
			if n != sz {
				b.Fatalf("size mismatch: want %d, got %d", sz, n)
			}
			resp.Body.Close()
		}
		b.ReportMetric(100*float64(len(bin))/float64(len(raw)), "pct")
	})
	b.Run("zstd", func(b *testing.B) {
		c := http.Client{Transport: Transport(http.DefaultTransport)}

		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := c.Get(serverZstd.URL + "/zstd")
			if err != nil {
				b.Fatal(err)
			}
			n, err := io.Copy(io.Discard, resp.Body)
			if err != nil {
				b.Fatal(err)
			}
			if n != sz {
				b.Fatalf("size mismatch: want %d, got %d", sz, n)
			}
			resp.Body.Close()
		}
		b.ReportMetric(100*float64(len(zsBin))/float64(len(raw)), "pct")
	})
	b.Run("gzhttp-par", func(b *testing.B) {
		c := http.Client{
			Transport: Transport(&http.Transport{
				MaxConnsPerHost:     runtime.GOMAXPROCS(0),
				MaxIdleConnsPerHost: runtime.GOMAXPROCS(0),
			}),
		}

		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := c.Get(server.URL + "/gzipped")
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.Copy(io.Discard, resp.Body)
				if err != nil {
					b.Fatal(err)
				}
				if n != sz {
					b.Fatalf("size mismatch: want %d, got %d", sz, n)
				}
				resp.Body.Close()
			}
		})
		b.ReportMetric(100*float64(len(bin))/float64(len(raw)), "pct")
	})
	b.Run("stdlib-par", func(b *testing.B) {
		c := http.Client{Transport: &http.Transport{
			MaxConnsPerHost:     runtime.GOMAXPROCS(0),
			MaxIdleConnsPerHost: runtime.GOMAXPROCS(0),
		}}
		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := c.Get(server.URL + "/gzipped")
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.Copy(io.Discard, resp.Body)
				if err != nil {
					b.Fatal(err)
				}
				if n != sz {
					b.Fatalf("size mismatch: want %d, got %d", sz, n)
				}
				resp.Body.Close()
			}
		})
		b.ReportMetric(100*float64(len(bin))/float64(len(raw)), "pct")
	})
	b.Run("zstd-par", func(b *testing.B) {
		c := http.Client{
			Transport: Transport(&http.Transport{
				MaxConnsPerHost:     runtime.GOMAXPROCS(0),
				MaxIdleConnsPerHost: runtime.GOMAXPROCS(0),
			}),
		}

		b.SetBytes(int64(sz))
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := c.Get(serverZstd.URL + "/zstd")
				if err != nil {
					b.Fatal(err)
				}
				n, err := io.Copy(io.Discard, resp.Body)
				if err != nil {
					b.Fatal(err)
				}
				if n != sz {
					b.Fatalf("size mismatch: want %d, got %d", sz, n)
				}
				resp.Body.Close()
			}
		})
		b.ReportMetric(100*float64(len(zsBin))/float64(len(raw)), "pct")
	})
}
