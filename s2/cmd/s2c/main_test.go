package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestOutputAliasesInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(input, []byte("input"), 0o600); err != nil {
		t.Fatal(err)
	}
	different := filepath.Join(dir, "different.txt")
	if err := os.WriteFile(different, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		output func(*testing.T) string
		want   bool
	}{
		{
			name: "same path",
			output: func(*testing.T) string {
				return input
			},
			want: true,
		},
		{
			name: "normalized path",
			output: func(*testing.T) string {
				sep := string(os.PathSeparator)
				return filepath.Dir(input) + sep + "." + sep + filepath.Base(input)
			},
			want: true,
		},
		{
			name: "symbolic link",
			output: func(t *testing.T) string {
				name := filepath.Join(dir, "symlink.txt")
				if err := os.Symlink(input, name); err != nil {
					t.Skipf("symbolic links are unavailable: %v", err)
				}
				return name
			},
			want: true,
		},
		{
			name: "hard link",
			output: func(t *testing.T) string {
				name := filepath.Join(dir, "hardlink.txt")
				if err := os.Link(input, name); err != nil {
					t.Skipf("hard links are unavailable: %v", err)
				}
				return name
			},
			want: true,
		},
		{
			name: "different file",
			output: func(*testing.T) string {
				return different
			},
		},
		{
			name: "missing destination",
			output: func(*testing.T) string {
				return filepath.Join(dir, "missing.txt")
			},
		},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name   string
			output func(*testing.T) string
			want   bool
		}{
			name: "case-insensitive Windows path",
			output: func(*testing.T) string {
				return strings.ToUpper(input)
			},
			want: true,
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := outputAliasesInput(input, test.output(t))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("outputAliasesInput() = %v, want %v", got, test.want)
			}
		})
	}

	t.Run("source stat error", func(t *testing.T) {
		_, err := outputAliasesInput(filepath.Join(dir, "missing-input.txt"), input)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("outputAliasesInput() error = %v, want os.ErrNotExist", err)
		}
	})
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

func TestBlockOutputAliases(t *testing.T) {
	tests := []struct {
		name            string
		output          func(*testing.T, string) string
		preservesSource bool
	}{
		{
			name: "normalized path",
			output: func(_ *testing.T, input string) string {
				sep := string(os.PathSeparator)
				return filepath.Dir(input) + sep + "." + sep + filepath.Base(input)
			},
		},
		{
			name: "symbolic link",
			output: func(t *testing.T, input string) string {
				name := filepath.Join(filepath.Dir(input), "symlink.txt")
				if err := os.Symlink(input, name); err != nil {
					t.Skipf("symbolic links are unavailable: %v", err)
				}
				return name
			},
			preservesSource: true,
		},
		{
			name: "hard link",
			output: func(t *testing.T, input string) string {
				name := filepath.Join(filepath.Dir(input), "hardlink.txt")
				if err := os.Link(input, name); err != nil {
					t.Skipf("hard links are unavailable: %v", err)
				}
				return name
			},
			preservesSource: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("replace input", func(t *testing.T) {
				input := []byte("replace aliased input after successful block output")
				filename := writeTestInput(t, "input.txt", input)
				output := test.output(t, filename)

				runS2C(t, "-block", "-q", "-verify", "-o", output, filename)
				assertBlockDecodesTo(t, output, input)
				if test.preservesSource {
					got, err := os.ReadFile(filename)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, input) {
						t.Fatalf("source contents = %q, want %q", got, input)
					}
				}
				assertNoBlockTempFiles(t, output)
			})

			for _, flag := range []string{"-safe", "-rm"} {
				t.Run("rejects "+flag, func(t *testing.T) {
					input := []byte("preserve aliased input when replacement is rejected")
					filename := writeTestInput(t, "input.txt", input)
					output := test.output(t, filename)

					runS2CWantError(t, "-block", "-q", flag, "-o", output, filename)
					for _, name := range []string{filename, output} {
						got, err := os.ReadFile(name)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(got, input) {
							t.Fatalf("%s contents = %q, want %q", name, got, input)
						}
					}
					assertNoBlockTempFiles(t, output)
				})
			}
		})
	}
}

func TestBlockOutputFailureCleanup(t *testing.T) {
	const contents = "compressed block"
	writeErr := errors.New("injected write failure")
	closeErr := errors.New("injected close failure")

	t.Run("direct write failure leaves partial output", func(t *testing.T) {
		name := filepath.Join(t.TempDir(), "partial.s2.block")
		output, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		writer := &failAfterWriter{out: output, limit: 3, err: writeErr}

		err = writeBlockOutput(writer, output, "", []byte(contents))
		if !errors.Is(err, writeErr) {
			t.Fatalf("writeBlockOutput() error = %v, want %v", err, writeErr)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("partial output was removed: %v", err)
		}
		if want := contents[:3]; string(got) != want {
			t.Fatalf("partial output = %q, want %q", got, want)
		}
	})

	t.Run("temporary write failure removes output", func(t *testing.T) {
		dir := t.TempDir()
		finalName := filepath.Join(dir, "output.s2.block")
		output, err := os.CreateTemp(dir, "."+filepath.Base(finalName)+".tmp-*")
		if err != nil {
			t.Fatal(err)
		}
		tempName := output.Name()
		writer := &failAfterWriter{out: output, limit: 3, err: writeErr}

		err = writeBlockOutput(writer, output, tempName, []byte(contents))
		if !errors.Is(err, writeErr) {
			t.Fatalf("writeBlockOutput() error = %v, want %v", err, writeErr)
		}
		assertNoBlockTempFiles(t, finalName)
	})

	t.Run("temporary close failure removes output", func(t *testing.T) {
		dir := t.TempDir()
		finalName := filepath.Join(dir, "output.s2.block")
		output, err := os.CreateTemp(dir, "."+filepath.Base(finalName)+".tmp-*")
		if err != nil {
			t.Fatal(err)
		}
		tempName := output.Name()

		err = closeBlockOutput(&failCloser{closer: output, err: closeErr}, tempName)
		if !errors.Is(err, closeErr) {
			t.Fatalf("closeBlockOutput() error = %v, want %v", err, closeErr)
		}
		assertNoBlockTempFiles(t, finalName)
	})

	t.Run("foreign destination replacement remains", func(t *testing.T) {
		dir := t.TempDir()
		name := filepath.Join(dir, "output.s2.block")
		movedName := filepath.Join(dir, "partial-output")
		replacement := []byte("replacement from another writer")
		output, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		writer := &failAfterWriter{out: output, limit: 3, err: writeErr}
		closer := &replaceOnClose{
			file:        output,
			name:        name,
			movedName:   movedName,
			replacement: replacement,
			err:         closeErr,
		}

		err = writeBlockOutput(writer, closer, "", []byte(contents))
		if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
			t.Fatalf("writeBlockOutput() error = %v, want write and close errors", err)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("foreign replacement was removed: %v", err)
		}
		if !bytes.Equal(got, replacement) {
			t.Fatalf("foreign replacement = %q, want %q", got, replacement)
		}
	})
}

type failAfterWriter struct {
	out   io.Writer
	limit int
	err   error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit {
		p = p[:w.limit]
	}
	n, err := w.out.Write(p)
	if err != nil {
		return n, err
	}
	return n, w.err
}

type failCloser struct {
	closer io.Closer
	err    error
}

func (c *failCloser) Close() error {
	return errors.Join(c.closer.Close(), c.err)
}

type replaceOnClose struct {
	file        *os.File
	name        string
	movedName   string
	replacement []byte
	err         error
}

func (c *replaceOnClose) Close() error {
	if err := c.file.Close(); err != nil {
		return errors.Join(err, c.err)
	}
	if err := os.Rename(c.name, c.movedName); err != nil {
		return errors.Join(err, c.err)
	}
	if err := os.WriteFile(c.name, c.replacement, 0o600); err != nil {
		return errors.Join(err, c.err)
	}
	return c.err
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

func assertBlockDecodesTo(t *testing.T, filename string, want []byte) {
	t.Helper()
	compressed, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Decode(nil, compressed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decoded output = %q, want %q", got, want)
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
