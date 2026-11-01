package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestUntarExtractsRegularFile(t *testing.T) {
	setQuiet(t)
	dst := filepath.Join(t.TempDir(), "dst")
	archive := tarBytes(t, tar.Header{
		Name: "nested/file.txt",
		Mode: 0o640,
		Size: int64(len("contents")),
	}, "contents")

	if err := untar(dst, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contents" {
		t.Fatalf("extracted content = %q, want %q", got, "contents")
	}
}

func TestUntarRejectsPreexistingSymlinkEscape(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "link")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	archive := tarBytes(t, tar.Header{
		Name: "link/escaped.txt",
		Mode: 0o600,
		Size: int64(len("escaped")),
	}, "escaped")

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly followed a symbolic link outside the root")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

func TestUntarRejectsTraversal(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	archive := tarBytes(t, tar.Header{
		Name: "../escaped.txt",
		Mode: 0o600,
		Size: int64(len("escaped")),
	}, "escaped")

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly accepted a parent traversal")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

func TestUntarRejectsSymlinkRoot(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.Symlink(outside, dst); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	if err := untar(dst, bytes.NewReader(tarBytes(t, tar.Header{
		Name: "file.txt",
		Mode: 0o600,
		Size: 1,
	}, "x"))); err == nil {
		t.Fatal("extraction unexpectedly accepted a symbolic-link root")
	}
}

func tarBytes(t *testing.T, header tar.Header, contents string) []byte {
	t.Helper()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(contents)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func setQuiet(t *testing.T) {
	t.Helper()
	previous := *quiet
	*quiet = true
	t.Cleanup(func() {
		*quiet = previous
	})
}
