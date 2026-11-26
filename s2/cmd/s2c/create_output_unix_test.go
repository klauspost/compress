//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/klauspost/compress/s2/cmd/internal/fileio"
)

func TestCreateOutputRejectsFIFOAlias(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("fifo unavailable: %v", err)
	}
	inputInfo, err := os.Stat(fifo)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := fileio.CreateOutput(inputInfo, fifo, 0o600, false, false); err == nil {
		t.Fatal("fileio.CreateOutput() unexpectedly accepted a FIFO alias")
	}
}
