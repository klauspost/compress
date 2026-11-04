package main

import (
	"bytes"
	"testing"
)

func TestSwitchWriterSplitsOutput(t *testing.T) {
	var first, second bytes.Buffer
	writer := &switchWriter{w: &first, left: 3}
	writer.fn = func() {
		writer.w = &second
		writer.left = (1 << 63) - 1
	}

	if _, err := writer.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}

	if got := first.String(); got != "abc" {
		t.Fatalf("first output = %q, want abc", got)
	}
	if got := second.String(); got != "def" {
		t.Fatalf("second output = %q, want def", got)
	}
}
