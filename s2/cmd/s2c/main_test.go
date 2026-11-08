package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/s2"
)

const s2cTestProcess = "S2C_TEST_PROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(s2cTestProcess) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func TestBlockOutput(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		input := []byte("block file output")
		filename := writeTestInput(t, "input.txt", input)

		runS2C(t, "-block", "-q", "-verify", filename)

		compressed, err := os.ReadFile(filename + ".s2.block")
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := s2.Decode(nil, compressed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("decoded output = %q, want %q", decoded, input)
		}
	})

	t.Run("replace input", func(t *testing.T) {
		input := []byte("replace input after successful block output")
		filename := writeTestInput(t, "replace.txt", input)

		runS2C(t, "-block", "-q", "-verify", "-o", filename, filename)

		compressed, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := s2.Decode(nil, compressed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("decoded output = %q, want %q", decoded, input)
		}
		assertNoBlockTempFiles(t, filename)
	})

	for _, flag := range []string{"-safe", "-rm"} {
		t.Run("replace input rejects "+flag, func(t *testing.T) {
			input := []byte("preserve input when replacement is rejected")
			filename := writeTestInput(t, "preserve.txt", input)

			runS2CWantError(t, "-block", "-q", flag, "-o", filename, filename)

			got, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, input) {
				t.Fatalf("input contents = %q, want %q", got, input)
			}
			assertNoBlockTempFiles(t, filename)
		})
	}

	t.Run("remove source after success", func(t *testing.T) {
		input := []byte("remove source after successful output")
		filename := writeTestInput(t, "remove.txt", input)
		output := filename + ".s2.block"
		if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
			t.Fatal(err)
		}

		runS2CWantError(t, "-block", "-q", "-rm", "-safe", filename)
		if got, err := os.ReadFile(filename); err != nil {
			t.Fatalf("source was removed after failed output: %v", err)
		} else if !bytes.Equal(got, input) {
			t.Fatalf("source contents = %q, want %q", got, input)
		}

		if err := os.Remove(output); err != nil {
			t.Fatal(err)
		}
		runS2C(t, "-block", "-q", "-rm", filename)
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source still exists after successful output: %v", err)
		}
		compressed, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := s2.Decode(nil, compressed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("decoded output = %q, want %q", decoded, input)
		}
	})

	t.Run("multiple stdout outputs", func(t *testing.T) {
		first := []byte("first block")
		second := []byte("second block")
		firstFile := writeTestInput(t, "first.txt", first)
		secondFile := writeTestInput(t, "second.txt", second)

		got := runS2C(t, "-block", "-q", "-c", firstFile, secondFile)
		firstBlock := s2.EncodeBetter(nil, first)
		secondBlock := s2.EncodeBetter(nil, second)
		want := append(firstBlock, secondBlock...)
		if !bytes.Equal(got, want) {
			t.Fatalf("stdout length = %d, want %d", len(got), len(want))
		}
	})
}

func TestWriteBlockOutputRemovesPartialFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "partial.s2.block")
	output, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := &failAfterWriter{out: output, limit: 3}

	err = writeBlockOutput(writer, output, name, []byte("compressed block"))
	if err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial output still exists: %v", err)
	}
}

type failAfterWriter struct {
	out   io.Writer
	limit int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	n, err := w.out.Write(p)
	if err != nil {
		return n, err
	}
	return n, errors.New("injected write failure")
}

func writeTestInput(t *testing.T, name string, contents []byte) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func assertNoBlockTempFiles(t *testing.T, filename string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), "."+filepath.Base(filename)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary output remains: %v", matches)
	}
}

func runS2C(t *testing.T, args ...string) []byte {
	t.Helper()
	stdout, stderr, err := runS2CProcess(t, args...)
	if err != nil {
		t.Fatalf("s2c %v failed: %v\n%s", args, err, stderr)
	}
	return stdout
}

func runS2CWantError(t *testing.T, args ...string) {
	t.Helper()
	_, stderr, err := runS2CProcess(t, args...)
	if err == nil {
		t.Fatalf("s2c %v unexpectedly succeeded\n%s", args, stderr)
	}
}

func runS2CProcess(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), s2cTestProcess+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
