package main

import (
	"bytes"
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
		output          func(*testing.T, string) string
		preservesSource bool
	}{
		{
			name: "same path",
			output: func(_ *testing.T, input string) string {
				return input
			},
		},
		{
			name: "normalized path",
			output: func(_ *testing.T, input string) string {
				separator := string(os.PathSeparator)
				return filepath.Dir(input) + separator + "." + separator + filepath.Base(input)
			},
		},
		{
			name: "symbolic link",
			output: func(t *testing.T, input string) string {
				name := filepath.Join(filepath.Dir(input), "output.txt")
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
				name := filepath.Join(filepath.Dir(input), "output.txt")
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
			t.Run("replaces output after successful decode", func(t *testing.T) {
				want := []byte("decoded output")
				input, compressed := writeS2Input(t, want)
				output := test.output(t, input)

				runS2D(t, "-q", "-o", output, input)

				got, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("output contents = %q, want %q", got, want)
				}
				if test.preservesSource {
					got, err := os.ReadFile(input)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, compressed) {
						t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
					}
				}
				assertNoOutputTempFiles(t, output)
			})

			for _, flag := range []string{"-safe", "-rm"} {
				t.Run("rejects "+flag, func(t *testing.T) {
					want := []byte("preserve aliased source")
					input, compressed := writeS2Input(t, want)
					output := test.output(t, input)

					runS2DWantError(t, "-q", flag, "-o", output, input)

					for _, name := range []string{input, output} {
						got, err := os.ReadFile(name)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(got, compressed) {
							t.Fatalf("%s contents changed: got %d bytes, want %d", name, len(got), len(compressed))
						}
					}
					assertNoOutputTempFiles(t, output)
				})
			}
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
			input := filepath.Join(t.TempDir(), "invalid.s2")
			contents := []byte("not valid s2 data")
			if err := os.WriteFile(input, contents, 0o600); err != nil {
				t.Fatal(err)
			}

			args := append([]string{"-q"}, test.args...)
			args = append(args, "-o", input, input)
			runS2DWantError(t, args...)

			got, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, contents) {
				t.Fatalf("input contents = %q, want %q", got, contents)
			}
			assertNoOutputTempFiles(t, input)
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
			want := []byte("decoded derived output")
			input, compressed := writeS2Input(t, want)
			output := strings.TrimSuffix(input, s2Ext)
			if err := test.link(input, output); err != nil {
				t.Skipf("%s is unavailable: %v", test.name, err)
			}

			runS2D(t, "-q", input)

			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output contents = %q, want %q", got, want)
			}
			got, err = os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, compressed) {
				t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
			}
			assertNoOutputTempFiles(t, output)
		})
	}
}

func TestBlockAliasedOutput(t *testing.T) {
	want := []byte("decoded block output")
	input := filepath.Join(t.TempDir(), "input.s2")
	if err := os.WriteFile(input, s2.Encode(nil, want), 0o600); err != nil {
		t.Fatal(err)
	}

	runS2D(t, "-q", "-block", "-o", input, input)

	got, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output contents = %q, want %q", got, want)
	}
	assertNoOutputTempFiles(t, input)
}

func TestDistinctOutputOverwritesExistingFile(t *testing.T) {
	want := []byte("decoded distinct output")
	input, compressed := writeS2Input(t, want)
	output := filepath.Join(t.TempDir(), "output.txt")
	if err := os.WriteFile(output, []byte("existing output that is longer"), 0o600); err != nil {
		t.Fatal(err)
	}

	runS2D(t, "-q", "-o", output, input)

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output contents = %q, want %q", got, want)
	}
	got, err = os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, compressed) {
		t.Fatalf("source contents changed: got %d bytes, want %d", len(got), len(compressed))
	}
	assertNoOutputTempFiles(t, output)
}

func writeS2Input(t *testing.T, contents []byte) (string, []byte) {
	t.Helper()
	var encoded bytes.Buffer
	writer := s2.NewWriter(&encoded)
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "input.s2")
	if err := os.WriteFile(name, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return name, bytes.Clone(encoded.Bytes())
}

func assertNoOutputTempFiles(t *testing.T, filename string) {
	t.Helper()
	for _, suffix := range []string{".tmp-*", ".backup-*"} {
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(filename), "."+filepath.Base(filename)+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("temporary output remains: %v", matches)
		}
	}
}

func runS2D(t *testing.T, args ...string) {
	t.Helper()
	_, stderr, err := runS2DProcess(t, args...)
	if err != nil {
		t.Fatalf("s2d %v failed: %v\n%s", args, err, stderr)
	}
}

func runS2DWantError(t *testing.T, args ...string) {
	t.Helper()
	_, stderr, err := runS2DProcess(t, args...)
	if err == nil {
		t.Fatalf("s2d %v unexpectedly succeeded\n%s", args, stderr)
	}
}

func runS2DProcess(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()
	command := exec.Command(os.Args[0], args...)
	command.Env = append(os.Environ(), s2dTestProcess+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func TestSafeOutputDoesNotReplaceExistingFile(t *testing.T) {
	input, _ := writeS2Input(t, []byte("safe output"))
	output := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(output, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	runS2DWantError(t, "-q", "-safe", "-o", output, input)

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("existing output changed: %q", got)
	}
	if _, err := os.Stat(input); err != nil {
		t.Fatalf("source changed after output creation failed: %v", err)
	}
}
