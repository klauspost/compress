package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/s2"
)

const s2dTestProcess = "S2D_TEST_PROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(s2dTestProcess) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func compressStream(data []byte) []byte {
	var buf bytes.Buffer
	w := s2.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func compressBlock(data []byte) []byte {
	return s2.Encode(nil, data)
}

func runS2D(t *testing.T, args ...string) {
	t.Helper()
	stdout, stderr, err := runS2DProcess(t, args...)
	if err != nil {
		t.Fatalf("s2d %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
	}
}

func runS2DFail(t *testing.T, args ...string) {
	t.Helper()
	_, stderr, err := runS2DProcess(t, args...)
	if err == nil {
		t.Fatalf("s2d %v unexpectedly succeeded\nstderr:\n%s", args, stderr)
	}
}

func runS2DProcess(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), s2dTestProcess+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func writeTestCompressedFile(t *testing.T, name string, raw []byte, isBlock bool) string {
	t.Helper()
	var compressed []byte
	if isBlock {
		compressed = compressBlock(raw)
	} else {
		compressed = compressStream(raw)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, compressed, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecompressAliasedOutput(t *testing.T) {
	raw := []byte("decompression of aliased input and output data payload")

	tests := []struct {
		name   string
		block  bool
		output func(t *testing.T, input string) string
	}{
		{
			name: "stream same path",
			output: func(_ *testing.T, input string) string {
				return input
			},
		},
		{
			name: "stream normalized path",
			output: func(_ *testing.T, input string) string {
				sep := string(os.PathSeparator)
				return filepath.Dir(input) + sep + "." + sep + filepath.Base(input)
			},
		},
		{
			name: "stream hard link",
			output: func(t *testing.T, input string) string {
				link := filepath.Join(filepath.Dir(input), "hardlink.s2")
				if err := os.Link(input, link); err != nil {
					t.Skipf("hard links unavailable: %v", err)
				}
				return link
			},
		},
		{
			name:  "block same path",
			block: true,
			output: func(_ *testing.T, input string) string {
				return input
			},
		},
		{
			name:  "block normalized path",
			block: true,
			output: func(_ *testing.T, input string) string {
				sep := string(os.PathSeparator)
				return filepath.Dir(input) + sep + "." + sep + filepath.Base(input)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputPath := writeTestCompressedFile(t, "input.txt.s2", raw, tt.block)
			outputPath := tt.output(t, inputPath)

			args := []string{"-q"}
			if tt.block {
				args = append(args, "-block")
			}
			args = append(args, "-o", outputPath, inputPath)
			runS2D(t, args...)

			got, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("failed reading output: %v", err)
			}
			if !bytes.Equal(got, raw) {
				t.Fatalf("output = %q, want %q", got, raw)
			}
		})
	}
}

func TestDerivedNameAliasedOutput(t *testing.T) {
	raw := []byte("decompression derived output aliased to source")
	dir := t.TempDir()
	source := filepath.Join(dir, "data.txt.s2")
	compressed := compressStream(raw)
	if err := os.WriteFile(source, compressed, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create derived output path as a hard link to the source file
	derived := filepath.Join(dir, "data.txt")
	if err := os.Link(source, derived); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	runS2D(t, "-q", source)

	got, err := os.ReadFile(derived)
	if err != nil {
		t.Fatalf("failed reading derived output: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("derived output = %q, want %q", got, raw)
	}
}

func TestAliasedOutputRejectsFlags(t *testing.T) {
	raw := []byte("payload for flag rejection testing")
	inputPath := writeTestCompressedFile(t, "input.txt.s2", raw, false)

	t.Run("rejects -safe", func(t *testing.T) {
		runS2DFail(t, "-q", "-safe", "-o", inputPath, inputPath)
		// Ensure file was not modified
		compressed := compressStream(raw)
		current, err := os.ReadFile(inputPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, compressed) {
			t.Fatalf("file was modified despite -safe rejection")
		}
	})

	t.Run("rejects -rm", func(t *testing.T) {
		runS2DFail(t, "-q", "-rm", "-o", inputPath, inputPath)
		// Ensure file was not deleted or modified
		compressed := compressStream(raw)
		current, err := os.ReadFile(inputPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, compressed) {
			t.Fatalf("file was modified despite -rm rejection")
		}
	})
}

func TestAliasedOutputPreservesSourceOnFailure(t *testing.T) {
	corruptData := []byte("this is not valid s2 compressed data")
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "corrupt.txt.s2")
	if err := os.WriteFile(inputPath, corruptData, 0o600); err != nil {
		t.Fatal(err)
	}

	runS2DFail(t, "-q", "-o", inputPath, inputPath)

	// Ensure corrupt input is still intact
	got, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corruptData) {
		t.Fatalf("corrupt source was truncated or modified on failure")
	}

	// Ensure no leftover temp files
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "corrupt.txt.s2" {
		t.Fatalf("found unexpected files in directory after failure: %v", entries)
	}
}

func TestDistinctOutput(t *testing.T) {
	raw := []byte("distinct output payload")
	inputPath := writeTestCompressedFile(t, "distinct.txt.s2", raw, false)
	outputPath := filepath.Join(filepath.Dir(inputPath), "distinct.txt")

	runS2D(t, "-q", "-o", outputPath, inputPath)

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("got %q, want %q", got, raw)
	}

	t.Run("remove source with distinct output", func(t *testing.T) {
		in := writeTestCompressedFile(t, "removeme.txt.s2", raw, false)
		out := filepath.Join(filepath.Dir(in), "removeme.txt")

		runS2D(t, "-q", "-rm", "-o", out, in)

		if _, err := os.Stat(in); !os.IsNotExist(err) {
			t.Fatalf("source file was not removed")
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("output corrupted: got %q, want %q", got, raw)
		}
	})
}
