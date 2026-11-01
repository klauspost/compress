package fileio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCreateExclusiveRejectsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "output")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	output, err := Create(link, 0o666, true)
	if output != nil {
		_ = output.Abort()
		t.Fatal("exclusive creation unexpectedly returned an output")
	}
	if err == nil {
		t.Fatal("exclusive creation unexpectedly followed a dangling symbolic link")
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symbolic link target was created: %v", err)
	}
}

func TestCreateStripsSpecialModeBits(t *testing.T) {
	name := filepath.Join(t.TempDir(), "output")
	output, err := Create(name, 0o755|os.ModeSetuid|os.ModeSetgid|os.ModeSticky, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = output.Abort()
	})
	staged, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := staged.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("output mode while writing = %v, want 0600", got)
	}

	if _, err := output.Write([]byte("output")); err != nil {
		t.Fatal(err)
	}
	if err := output.Commit(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if special := info.Mode() & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky); special != 0 {
		t.Fatalf("output retained special mode bits: %v", special)
	}
}

func TestCreateTempCommitsInDestinationDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "archive.s2")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := CreateTemp(target, 0o750|os.ModeSetuid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = output.Abort()
	})
	tempName := output.Name()

	if filepath.Dir(tempName) != dir {
		t.Fatalf("temporary output created in %q, want %q", filepath.Dir(tempName), dir)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("target changed before commit: %q, %v", got, err)
	}
	if _, err := output.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := output.Commit(); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("committed target = %q, %v", got, err)
	}
	if _, err := os.Stat(tempName); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary output remains after commit: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("committed target retained set-ID mode: %v", info.Mode())
	}
}

func TestAbortRemovesTemporaryOutput(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "archive.s2")
	output, err := CreateTemp(target, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	tempName := output.Name()

	if err := output.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempName); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("temporary output remains after abort: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("target exists after abort: %v", err)
	}
}
