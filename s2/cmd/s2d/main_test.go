package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestOutputAliases(t *testing.T) {
	tests := []struct {
		name            string
		output          func(*testing.T, string, string) string
		preservesSource bool
	}{
		{
			name: "same path",
			output: func(_ *testing.T, _, input string) string {
				return input
			},
		},
		{
			name: "normalized path",
			output: func(_ *testing.T, _, input string) string {
				separator := string(os.PathSeparator)
				return "." + separator + input
			},
		},
		{
			name: "symbolic link",
			output: func(t *testing.T, dir, input string) string {
				name := "output.txt"
				if err := os.Symlink(input, filepath.Join(dir, name)); err != nil {
					t.Skipf("symbolic links are unavailable: %v", err)
				}
				return name
			},
			preservesSource: true,
		},
		{
			name: "hard link",
			output: func(t *testing.T, dir, input string) string {
				name := "output.txt"
				if err := os.Link(filepath.Join(dir, input), filepath.Join(dir, name)); err != nil {
					t.Skipf("hard links are unavailable: %v", err)
				}
				return name
			},
			preservesSource: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Run("replaces output after successful decode", func(t *testing.T) {
				dir := t.TempDir()
				want := []byte("decoded output")
				input, compressed := writeS2Input(t, dir, want)
				output := test.output(t, dir, input)

				runS2D(t, dir, "-q", "-o", output, input)

				got, err := os.ReadFile(filepath.Join(dir, output))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("output contents = %q, want %q", got, want)
				}
				if test.preservesSource {
					got, err := os.ReadFile(filepath.Join(dir, input))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, compressed) {
						t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
					}
				}
				assertNoOutputTempFiles(t, dir)
			})

			for _, flag := range []string{"-safe", "-rm"} {
				t.Run("rejects "+flag, func(t *testing.T) {
					dir := t.TempDir()
					want := []byte("preserve aliased source")
					input, compressed := writeS2Input(t, dir, want)
					output := test.output(t, dir, input)

					wantError := "cannot remove when input = output"
					if flag == "-safe" {
						wantError = "cannot use -safe when output path is the same as the input file"
					}
					runS2DWantError(t, dir, wantError, "-q", flag, "-o", output, input)

					for _, name := range []string{input, output} {
						got, err := os.ReadFile(filepath.Join(dir, name))
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(got, compressed) {
							t.Fatalf("%s contents changed: got %d bytes, want %d", name, len(got), len(compressed))
						}
					}
					assertNoOutputTempFiles(t, dir)
				})
			}

			t.Run("rejects stdin", func(t *testing.T) {
				dir := t.TempDir()
				input, compressed := writeS2Input(t, dir, []byte("preserve stdin source"))
				output := test.output(t, dir, input)
				stdin, err := os.Open(filepath.Join(dir, input))
				if err != nil {
					t.Fatal(err)
				}
				defer stdin.Close()

				_, stderr, err := runS2DProcess(t, dir, stdin, "-q", "-o", output, "-")
				if err == nil || !bytes.Contains(stderr, []byte("stdin and output refer to the same file")) {
					t.Fatalf("s2d error = %v, stderr = %q, want stdin alias rejection", err, stderr)
				}
				for _, name := range []string{input, output} {
					got, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, compressed) {
						t.Fatalf("%s contents changed: got %d bytes, want %d", name, len(got), len(compressed))
					}
				}
			})
		})
	}
}

func TestAliasedOutputFailurePreservesInput(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "stream"},
		{name: "block", args: []string{"-block"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			input := "invalid.s2"
			contents := []byte("not valid s2 data")
			if err := os.WriteFile(filepath.Join(dir, input), contents, 0o600); err != nil {
				t.Fatal(err)
			}

			args := append([]string{"-q"}, test.args...)
			args = append(args, "-o", input, input)
			runS2DWantError(t, dir, "s2: corrupt input", args...)

			got, err := os.ReadFile(filepath.Join(dir, input))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, contents) {
				t.Fatalf("input contents = %q, want %q", got, contents)
			}
			assertNoOutputTempFiles(t, dir)
		})
	}
}

func TestDerivedOutputAliasPreservesSource(t *testing.T) {
	for _, test := range []struct {
		name string
		link func(string, string) error
	}{
		{name: "symbolic link", link: os.Symlink},
		{name: "hard link", link: os.Link},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			want := []byte("decoded derived output")
			input, compressed := writeS2Input(t, dir, want)
			output := strings.TrimSuffix(input, s2Ext)
			if err := test.link(filepath.Join(dir, input), filepath.Join(dir, output)); err != nil {
				t.Skipf("%s is unavailable: %v", test.name, err)
			}

			runS2D(t, dir, "-q", input)

			got, err := os.ReadFile(filepath.Join(dir, output))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output contents = %q, want %q", got, want)
			}
			got, err = os.ReadFile(filepath.Join(dir, input))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, compressed) {
				t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
			}
			assertNoOutputTempFiles(t, dir)
		})
	}
}

func TestBlockAliasedOutput(t *testing.T) {
	dir := t.TempDir()
	want := []byte("decoded block output")
	input := "input.s2"
	if err := os.WriteFile(filepath.Join(dir, input), s2.Encode(nil, want), 0o600); err != nil {
		t.Fatal(err)
	}

	runS2D(t, dir, "-q", "-block", "-o", input, input)

	got, err := os.ReadFile(filepath.Join(dir, input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output contents = %q, want %q", got, want)
	}
	assertNoOutputTempFiles(t, dir)
}

func TestDistinctOutputOverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	want := []byte("decoded distinct output")
	input, compressed := writeS2Input(t, dir, want)
	output := "output.txt"
	if err := os.WriteFile(filepath.Join(dir, output), []byte("existing output that is longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	runS2D(t, dir, "-q", "-o", output, input)

	got, err := os.ReadFile(filepath.Join(dir, output))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output contents = %q, want %q", got, want)
	}
	got, err = os.ReadFile(filepath.Join(dir, input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, compressed) {
		t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
	}
	assertNoOutputTempFiles(t, dir)
}

func writeS2Input(t *testing.T, dir string, contents []byte) (string, []byte) {
	t.Helper()
	var encoded bytes.Buffer
	writer := s2.NewWriter(&encoded)
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	name := "input.s2"
	if err := os.WriteFile(filepath.Join(dir, name), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return name, bytes.Clone(encoded.Bytes())
}

func assertNoOutputTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".s2-tmp-") ||
			strings.HasPrefix(entry.Name(), ".s2-backup-") {
			t.Fatalf("temporary output remains: %s", entry.Name())
		}
	}
}

func runS2D(t *testing.T, dir string, args ...string) {
	t.Helper()
	_, stderr, err := runS2DProcess(t, dir, nil, args...)
	if err != nil {
		t.Fatalf("s2d %v failed: %v\n%s", args, err, stderr)
	}
}

func runS2DWantError(t *testing.T, dir, wantDiagnostic string, args ...string) {
	t.Helper()
	_, stderr, err := runS2DProcess(t, dir, nil, args...)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("s2d %v error = %v, want process exit error\n%s", args, err, stderr)
	}
	if exitError.ExitCode() != 2 {
		t.Fatalf("s2d %v exit code = %d, want 2\n%s", args, exitError.ExitCode(), stderr)
	}
	if !bytes.Contains(stderr, []byte(wantDiagnostic)) {
		t.Fatalf("s2d %v stderr = %q, want diagnostic containing %q", args, stderr, wantDiagnostic)
	}
}

func runS2DProcess(t *testing.T, dir string, stdin io.Reader, args ...string) ([]byte, []byte, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), s2dTestProcess+"=1")
	command.Stdin = stdin
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func TestSafeOutputDoesNotReplaceExistingFile(t *testing.T) {
	dir := t.TempDir()
	input, _ := writeS2Input(t, dir, []byte("safe output"))
	output := "output"
	if err := os.WriteFile(filepath.Join(dir, output), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Unix reports "file exists", Windows "The file exists."
	runS2DWantError(t, dir, "file exists", "-q", "-safe", "-o", output, input)

	got, err := os.ReadFile(filepath.Join(dir, output))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("existing output changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, input)); err != nil {
		t.Fatalf("source changed after output creation failed: %v", err)
	}
}
