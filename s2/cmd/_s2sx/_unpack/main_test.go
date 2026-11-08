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
	archive := tarBytes(t, tarEntry{
		header:   tar.Header{Name: "nested/file.txt", Mode: 0o640},
		contents: "contents",
	})

	if err := untar(dst, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contents" {
		t.Fatalf("extracted content = %q, want contents", got)
	}
}

func TestUntarRejectsTraversal(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	archive := tarBytes(t, tarEntry{
		header:   tar.Header{Name: "../escaped.txt", Mode: 0o600},
		contents: "escaped",
	})

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly accepted a parent traversal")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

func TestUntarRejectsPreexistingSymlinkEscape(t *testing.T) {
	setQuiet(t)
	requireSymlinks(t)
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
		t.Fatal(err)
	}
	archive := tarBytes(t, tarEntry{
		header:   tar.Header{Name: "link/escaped.txt", Mode: 0o600},
		contents: "escaped",
	})

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly followed a symbolic link outside the root")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

func TestUntarRejectsArchiveSymlinkWriteEscape(t *testing.T) {
	setQuiet(t)
	requireSymlinks(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	archive := tarBytes(t,
		tarEntry{header: tar.Header{Name: "d1", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0o777}},
		tarEntry{header: tar.Header{Name: "d1/d2", Typeflag: tar.TypeSymlink, Linkname: "..", Mode: 0o777}},
		tarEntry{
			header:   tar.Header{Name: "d1/d2/escaped.txt", Mode: 0o600},
			contents: "escaped",
		},
	)

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly wrote through an escaping symbolic link")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

func TestUntarAllowsSymlinkedRoot(t *testing.T) {
	setQuiet(t)
	requireSymlinks(t)
	dir := t.TempDir()
	realRoot := filepath.Join(dir, "real")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.Symlink(realRoot, dst); err != nil {
		t.Fatal(err)
	}
	archive := tarBytes(t, tarEntry{
		header:   tar.Header{Name: "file.txt", Mode: 0o600},
		contents: "x",
	})

	if err := untar(dst, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(realRoot, "file.txt")); err != nil || string(got) != "x" {
		t.Fatalf("file under symlinked root = %q, %v", got, err)
	}
}

func TestUntarPathMutationCannotWriteOutsideRoot(t *testing.T) {
	setQuiet(t)
	requireSymlinks(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarBytes(t,
		tarEntry{header: tar.Header{Name: "d1", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0o777}},
		tarEntry{header: tar.Header{Name: "d2/sub", Typeflag: tar.TypeDir, Mode: 0o755}},
		tarEntry{header: tar.Header{Name: "d1/d2/link", Typeflag: tar.TypeSymlink, Linkname: "sub/../../outside", Mode: 0o777}},
		tarEntry{header: tar.Header{Name: "d1", Typeflag: tar.TypeSymlink, Linkname: "nonexistent", Mode: 0o777}},
		tarEntry{header: tar.Header{Name: "d2/sub", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0o777}},
		tarEntry{
			header:   tar.Header{Name: "d2/link/escaped.txt", Mode: 0o600},
			contents: "escaped",
		},
	)

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly wrote through the mutated symbolic-link path")
	}
	link := filepath.Join(dst, "d2", "link")
	if target, err := os.Readlink(link); err != nil {
		t.Fatalf("final symbolic link is missing: %v", err)
	} else if target != filepath.FromSlash("sub/../../outside") {
		t.Fatalf("final symbolic link target = %q", target)
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file was written outside the extraction root: %v", err)
	}
}

type tarEntry struct {
	header   tar.Header
	contents string
}

func tarBytes(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for i := range entries {
		header := entries[i].header
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			header.Size = int64(len(entries[i].contents))
		}
		if err := tw.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entries[i].contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func requireSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
}

func setQuiet(t *testing.T) {
	t.Helper()
	previous := *quiet
	*quiet = true
	t.Cleanup(func() {
		*quiet = previous
	})
}
