package fileio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateExclusiveRejectsExistingFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := Create(name, 0o666, true)
	if output != nil {
		_ = output.Close()
		t.Fatal("exclusive creation unexpectedly returned a file")
	}
	if err == nil {
		t.Fatal("exclusive creation unexpectedly replaced an existing file")
	}
	if got, err := os.ReadFile(name); err != nil || string(got) != "old" {
		t.Fatalf("existing output changed: %q, %v", got, err)
	}
}

func TestCreateExclusiveRejectsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "output")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	output, err := Create(link, 0o666, true)
	if output != nil {
		_ = output.Close()
		t.Fatal("exclusive creation unexpectedly returned a file")
	}
	if err == nil {
		t.Fatal("exclusive creation unexpectedly followed a dangling symbolic link")
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("symbolic link target was created: %v", err)
	}
}

func TestCreateNonExclusiveFollowsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "output")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	output, err := Create(link, 0o666, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("new")); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new" {
		t.Fatalf("symlink target = %q, %v", got, err)
	}
}

func TestCreateStripsSpecialModeBits(t *testing.T) {
	name := filepath.Join(t.TempDir(), "output")
	output, err := Create(name, 0o755|os.ModeSetuid|os.ModeSetgid|os.ModeSticky, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
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
