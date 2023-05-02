//go:build go1.20
// +build go1.20

package gzhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"testing"

	"github.com/klauspost/compress/gzip"
)

// This test is an adapted version of net/http/httputil.Test1xxResponses test.
func Test1xxResponses(t *testing.T) {
	wrapper, _ := NewWrapper()
	handler := wrapper(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Add("Link", "</style.css>; rel=preload; as=style")
			h.Add("Link", "</script.js>; rel=preload; as=script")
			w.WriteHeader(http.StatusEarlyHints)

			h.Add("Link", "</foo.js>; rel=preload; as=script")
			w.WriteHeader(http.StatusProcessing)

			w.Write(testBody)
		},
	))

	frontend := httptest.NewServer(handler)
	defer frontend.Close()
	frontendClient := frontend.Client()

	checkLinkHeaders := func(t *testing.T, expected, got []string) {
		t.Helper()

		if len(expected) != len(got) {
			t.Errorf("Expected %d link headers; got %d", len(expected), len(got))
		}

		for i := range expected {
			if i >= len(got) {
				t.Errorf("Expected %q link header; got nothing", expected[i])

				continue
			}

			if expected[i] != got[i] {
				t.Errorf("Expected %q link header; got %q", expected[i], got[i])
			}
		}
	}

	var respCounter uint8
	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			switch code {
			case http.StatusEarlyHints:
				checkLinkHeaders(t, []string{"</style.css>; rel=preload; as=style", "</script.js>; rel=preload; as=script"}, header["Link"])
			case http.StatusProcessing:
				checkLinkHeaders(t, []string{"</style.css>; rel=preload; as=style", "</script.js>; rel=preload; as=script", "</foo.js>; rel=preload; as=script"}, header["Link"])
			default:
				t.Error("Unexpected 1xx response")
			}

			respCounter++

			return nil
		},
	}
	req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), "GET", frontend.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")

	res, err := frontendClient.Do(req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	defer res.Body.Close()

	if respCounter != 2 {
		t.Errorf("Expected 2 1xx responses; got %d", respCounter)
	}
	checkLinkHeaders(t, []string{"</style.css>; rel=preload; as=style", "</script.js>; rel=preload; as=script", "</foo.js>; rel=preload; as=script"}, res.Header["Link"])

	assertEqual(t, "gzip", res.Header.Get("Content-Encoding"))

	body, _ := io.ReadAll(res.Body)
	assertEqual(t, gzipStrLevel(testBody, gzip.DefaultCompression), body)
}
