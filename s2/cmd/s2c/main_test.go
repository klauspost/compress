package main

import (
	"bytes"
	"errors"
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

func writeTestInput(t *testing.T, name string, contents []byte) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filename, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
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
