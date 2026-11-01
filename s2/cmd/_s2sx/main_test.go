package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/s2/cmd/internal/fileio"
)

func TestSwitchWriterCommitsBothOutputs(t *testing.T) {
	dir := t.TempDir()
	firstName := filepath.Join(dir, "archive.s2sx")
	secondName := firstName + ".more"
	first, err := fileio.Create(firstName, 0o600, true)
	if err != nil {
		t.Fatal(err)
	}
	writer := &switchWriter{output: first, left: 3}
	writer.next = func() (*fileio.Output, error) {
		return fileio.Create(secondName, 0o600, true)
	}
	t.Cleanup(func() {
		_ = writer.Abort()
	})

	if _, err := writer.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(firstName); err != nil || string(got) != "abc" {
		t.Fatalf("first output = %q, %v", got, err)
	}
	if got, err := os.ReadFile(secondName); err != nil || string(got) != "def" {
		t.Fatalf("second output = %q, %v", got, err)
	}
}
