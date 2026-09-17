package gzhttp

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestContentEncodingOrder(t *testing.T) {
	gz := func(b []byte) []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}
	fl := func(b []byte) []byte {
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, 5)
		w.Write(b)
		w.Close()
		return buf.Bytes()
	}

	for _, tc := range []struct {
		name     string
		sent     string
		body     []byte
		wantCE   string
		wantBody []byte
	}{{
		name: "gzip outermost keeps inner coding",
		sent: "deflate, gzip", body: gz(fl([]byte("hi"))),
		wantCE: "deflate", wantBody: fl([]byte("hi")),
	}, {
		name: "gzip not outermost is left alone",
		sent: "gzip, deflate", body: fl(gz([]byte("hi"))),
		wantCE: "gzip, deflate", wantBody: fl(gz([]byte("hi"))),
	}, {
		name: "three codings gzip outermost",
		sent: "br, deflate, gzip", body: gz([]byte("hi")),
		wantCE: "br, deflate", wantBody: []byte("hi"),
	}, {
		name: "single gzip is removed",
		sent: "gzip", body: gz([]byte("hi")),
		wantCE: "", wantBody: []byte("hi"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var gotCE string
			var gotBody []byte
			var readErr error
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotCE = r.Header.Get("Content-Encoding")
				gotBody, readErr = io.ReadAll(r.Body)
			})
			wrapper, err := NewWrapper(AllowCompressedRequests(true))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", bytes.NewReader(tc.body))
			req.Header.Set("Content-Encoding", tc.sent)
			wrapper(h).ServeHTTP(httptest.NewRecorder(), req)

			if readErr != nil {
				t.Errorf("body was decoded with the wrong coding: %v", readErr)
			}
			if gotCE != tc.wantCE {
				t.Errorf("Content-Encoding = %q, want %q", gotCE, tc.wantCE)
			}
			if !bytes.Equal(gotBody, tc.wantBody) {
				t.Errorf("body = %x, want %x", gotBody, tc.wantBody)
			}
		})
	}
}
