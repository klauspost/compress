package gzhttp

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	hi := []byte("hi")

	// Every body below carries exactly the codings its Content-Encoding declares,
	// applied in the order they are listed.
	for _, tc := range []struct {
		name     string
		sent     []string
		body     []byte
		wantCE   []string
		wantBody []byte
	}{{
		name: "gzip outermost keeps the inner coding",
		sent: []string{"deflate, gzip"}, body: gz(fl(hi)),
		wantCE: []string{"deflate"}, wantBody: fl(hi),
	}, {
		name: "gzip not outermost is left alone",
		sent: []string{"gzip, deflate"}, body: fl(gz(hi)),
		wantCE: []string{"gzip, deflate"}, wantBody: fl(gz(hi)),
	}, {
		name: "only the outermost of three gzip layers is removed",
		sent: []string{"gzip, gzip, gzip"}, body: gz(gz(gz(hi))),
		wantCE: []string{"gzip, gzip"}, wantBody: gz(gz(hi)),
	}, {
		name: "a single gzip removes the header",
		sent: []string{"gzip"}, body: gz(hi),
		wantCE: nil, wantBody: hi,
	}, {
		// One list split over two field lines means the same as one line.
		name: "split field lines gzip outermost",
		sent: []string{"deflate", "gzip"}, body: gz(fl(hi)),
		wantCE: []string{"deflate"}, wantBody: fl(hi),
	}, {
		name: "split field lines gzip not outermost",
		sent: []string{"gzip", "deflate"}, body: fl(gz(hi)),
		wantCE: []string{"gzip", "deflate"}, wantBody: fl(gz(hi)),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var gotCE []string
			var gotBody []byte
			var readErr error
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotCE = r.Header.Values("Content-Encoding")
				gotBody, readErr = io.ReadAll(r.Body)
			})
			wrapper, err := NewWrapper(AllowCompressedRequests(true))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", bytes.NewReader(tc.body))
			for _, v := range tc.sent {
				req.Header.Add("Content-Encoding", v)
			}
			wrapper(h).ServeHTTP(httptest.NewRecorder(), req)

			if readErr != nil {
				t.Errorf("body was decoded with the wrong coding: %v", readErr)
			}
			if !reflect.DeepEqual(gotCE, tc.wantCE) {
				t.Errorf("Content-Encoding = %q, want %q", gotCE, tc.wantCE)
			}
			if !bytes.Equal(gotBody, tc.wantBody) {
				t.Errorf("body = %x, want %x", gotBody, tc.wantBody)
			}
		})
	}
}
