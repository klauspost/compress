//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCreateOutputRejectsFIFOAlias(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("fifo unavailable: %v", err)
	}
	input, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	inputInfo, err := input.Stat()
	if err != nil {
		input.Close()
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := createOutput(inputInfo, fifo, 0o600, false, false); err == nil {
		t.Fatal("createOutput() unexpectedly accepted a FIFO alias")
	}
}
