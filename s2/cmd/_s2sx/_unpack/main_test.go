package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

func TestUntarRejectsSymlinkEscapeViaArchive(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	archive, err := tarArchive(
		tar.Header{Name: "d1", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0o777},
		tar.Header{Name: "d1/d2/", Typeflag: tar.TypeDir, Mode: 0o755},
		tar.Header{Name: "d1/d2/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly accepted an escaping symbolic link")
	}
	if _, err := os.Lstat(filepath.Join(dst, "d2", "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("escaping symlink left under extraction root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "link")); err == nil {
		t.Fatal("escaping symbolic link was created outside the extraction root")
	}
}

func TestUntarRejectsDeferredSymlinkEscape(t *testing.T) {
	setQuiet(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Link target sub/../../outside resolves to a file named "outside" inside the
	// extraction root while d2/sub is a directory; replacing d2/sub with a symlink
	// makes the same link target escape to dir/outside.
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	outsideFile := tar.Header{Name: "outside", Mode: 0o644, Size: 1}
	if err := tw.WriteHeader(&outsideFile); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	for _, hdr := range []tar.Header{
		{Name: "d2/sub/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "d2/link", Typeflag: tar.TypeSymlink, Linkname: "sub/../../outside", Mode: 0o777},
		{Name: "d2/sub", Typeflag: tar.TypeSymlink, Linkname: ".", Mode: 0o777},
	} {
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := untar(dst, bytes.NewReader(archive.Bytes())); err == nil {
		t.Fatal("extraction unexpectedly accepted a deferred escaping symbolic link")
	}
	if _, err := os.Lstat(filepath.Join(dst, "d2", "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("deferred escaping symlink left under extraction root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "link")); err == nil {
		t.Fatal("deferred escaping symbolic link was created outside the extraction root")
	}
}

func TestUntarRejectsHardLinkToSymlink(t *testing.T) {
	setQuiet(t)
	dst := filepath.Join(t.TempDir(), "dst")
	archive, err := tarArchive(
		tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "file.txt", Mode: 0o777},
		tar.Header{Name: "alias", Typeflag: tar.TypeLink, Linkname: "link", Mode: 0o644},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := untar(dst, bytes.NewReader(archive)); err == nil {
		t.Fatal("extraction unexpectedly accepted a hard link to a symbolic link")
	}
}

func TestUntarAllowsSymlinkBeforeTarget(t *testing.T) {
	setQuiet(t)
	dst := filepath.Join(t.TempDir(), "dst")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "alias", Typeflag: tar.TypeSymlink, Linkname: "file.txt", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	hdr := tar.Header{Name: "file.txt", Mode: 0o644, Size: 5}
	if err := tw.WriteHeader(&hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := untar(dst, bytes.NewReader(buf.Bytes())); err != nil {
		if strings.Contains(err.Error(), "making symbolic link") {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file.txt = %q, want hello", got)
	}
	linkTarget, err := os.Readlink(filepath.Join(dst, "alias"))
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != "file.txt" {
		t.Fatalf("alias link = %q, want file.txt", linkTarget)
	}
}

func tarArchive(headers ...tar.Header) ([]byte, error) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for i := range headers {
		if err := tw.WriteHeader(&headers[i]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
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
