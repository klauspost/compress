// Package silesiatest gives the packages' tests the Silesia corpus that
// testdata/fetch_silesia.sh fetches into the repository's testdata directory.
// Paths are relative to a package directory one level below the root.
package silesiatest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	once sync.Once
	data []byte
	err  error
)

// Tar returns silesia.tar, read once per test binary. It skips the test with
// -short or when the file is absent. A package-local testdata/silesia.tar is
// used if the repository's is missing.
func Tar(tb testing.TB) []byte {
	tb.Helper()
	if testing.Short() {
		tb.Skip("Silesia is skipped with -short")
	}
	once.Do(func() {
		data, err = os.ReadFile("../testdata/silesia.tar")
		if os.IsNotExist(err) {
			data, err = os.ReadFile("testdata/silesia.tar")
		}
	})
	if os.IsNotExist(err) {
		tb.Skip("Missing testdata/silesia.tar; run testdata/fetch_silesia.sh")
	}
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// ZstdFiles lists the zstd encodings of silesia.tar that
// testdata/fetch_silesia.sh keeps and makes: silesia.tar*.zst.
func ZstdFiles() []string {
	files, _ := filepath.Glob("../testdata/silesia.tar*.zst")
	if len(files) == 0 {
		files, _ = filepath.Glob("testdata/silesia.tar*.zst")
	}
	return files
}
