//go:build unix

package fileio

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCreateStagesAndRestoresSanitizedMode(t *testing.T) {
	oldUmask := syscall.Umask(0o027)
	t.Cleanup(func() {
		syscall.Umask(oldUmask)
	})

	name := filepath.Join(t.TempDir(), "output")
	output, err := Create(name, 0o777|os.ModeSetuid|os.ModeSetgid, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = output.Abort()
	})

	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode while writing = %v, want 0600", got)
	}
	if _, err := output.Write([]byte("output")); err != nil {
		t.Fatal(err)
	}
	if err := output.Commit(); err != nil {
		t.Fatal(err)
	}

	info, err = os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("committed output mode = %v, want 0750", got)
	}
	if special := info.Mode() & (os.ModeSetuid | os.ModeSetgid); special != 0 {
		t.Fatalf("committed output retained special mode bits: %v", special)
	}
}

func TestCreatePreservesExistingMode(t *testing.T) {
	name := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(name, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o640); err != nil {
		t.Fatal(err)
	}

	output, err := Create(name, 0o755, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = output.Abort()
	})
	if _, err := output.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := output.Commit(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("existing output mode = %v, want 0640", got)
	}
}

func TestCommitSkipsSyncForDevice(t *testing.T) {
	output, err := Create("/dev/null", 0o666, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("discard")); err != nil {
		t.Fatal(err)
	}
	if err := output.Commit(); err != nil {
		t.Fatalf("commit to /dev/null: %v", err)
	}
}
