package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"testing"
	"testing/iotest"

	"github.com/klauspost/compress/s2"
)

func TestIsS2Input(t *testing.T) {
	for _, magic := range []string{"\xff\x06\x00\x00S2sTwO", "\xff\x06\x00\x00sNaPpY"} {
		inputs := []string{"ordinary input", magic, magic + "tail"}
		for n := range len(magic) {
			inputs = append(inputs, magic[:n])
			for b := range 256 {
				if byte(b) == magic[n] {
					continue
				}
				input := []byte(magic + "tail")
				input[n] = byte(b)
				inputs = append(inputs, string(input))
			}
		}
		for _, input := range inputs {
			want := input == magic || input == magic+"tail"
			for _, rd := range []io.Reader{bytes.NewBufferString(input), iotest.OneByteReader(bytes.NewBufferString(input))} {
				compressed, replay := isS2Input(rd)
				if compressed != want {
					t.Errorf("input %x: compressed = %v, want %v", input, compressed, want)
					return
				}
				got, err := io.ReadAll(replay)
				if err != nil || string(got) != input {
					t.Fatalf("input %x: replay = %x, %v", input, got, err)
				}
			}
		}
	}
}

func TestS2SXInput(t *testing.T) {
	if os.Getenv("S2SX_TEST_MAIN") == "1" {
		os.Args = append([]string{os.Args[0]}, flag.Args()...)
		main()
		os.Exit(0)
	}

	destOS, destArch := runtime.GOOS, runtime.GOARCH
	stub, err := embeddedFiles.ReadFile(path.Join("sfx-exe", destOS+"-"+destArch+".s2"))
	if os.IsNotExist(err) {
		destOS, destArch = "windows", "amd64"
		stub, err = embeddedFiles.ReadFile(path.Join("sfx-exe", destOS+"-"+destArch+".s2"))
	}
	if err != nil {
		t.Fatal(err)
	}
	stub, err = io.ReadAll(s2.NewReader(bytes.NewReader(stub)))
	if err != nil {
		t.Fatal(err)
	}
	type testCase struct {
		name       string
		input      []byte
		want       []byte
		compressed bool
	}
	tests := []testCase{{name: "empty"}, {name: "plain", input: []byte("ordinary input"), want: []byte("ordinary input")}}
	for _, magic := range []string{"\xff\x06\x00\x00S2sTwO", "\xff\x06\x00\x00sNaPpY"} {
		for n := range len(magic) {
			tests = append(tests, testCase{name: fmt.Sprintf("%s/truncated-%d", magic[4:], n), input: []byte(magic[:n]), want: []byte(magic[:n])})
		}
		for n := range len(magic) {
			input := []byte(magic + "ordinary tail")
			input[n] ^= 1
			tests = append(tests, testCase{name: fmt.Sprintf("%s/mismatch-%d", magic[4:], n), input: input, want: input})
		}
		tests = append(tests, testCase{name: magic[4:] + "/identifier", input: []byte(magic), compressed: true})
	}
	for _, snappy := range []bool{false, true} {
		input := []byte("already compressed input")
		var encoded bytes.Buffer
		opts := []s2.WriterOption{s2.WriterConcurrency(1)}
		if snappy {
			opts = append(opts, s2.WriterSnappyCompat())
		}
		w := s2.NewWriter(&encoded, opts...)
		if _, err := w.Write(input); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, testCase{name: fmt.Sprintf("compressed-snappy-%v", snappy), input: encoded.Bytes(), want: input, compressed: true})
	}
	for _, tt := range tests {
		for _, mode := range []string{"file", "remove", "stdin"} {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				name := filepath.Join(dir, "input.bin")
				args := []string{"-test.run=^TestS2SXInput$", "--", "-q", "-cpu=1", "-os=" + destOS, "-arch=" + destArch}
				if mode == "remove" {
					args = append(args, "-rm")
				}
				if mode == "stdin" {
					args = append(args, "-")
				} else {
					if err := os.WriteFile(name, tt.input, 0600); err != nil {
						t.Fatal(err)
					}
					args = append(args, name)
				}
				cmd := exec.Command(os.Args[0], args...)
				cmd.Env = append(os.Environ(), "S2SX_TEST_MAIN=1")
				cmd.Stdin = bytes.NewReader(tt.input)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				archive, err := cmd.Output()
				if err != nil {
					t.Fatalf("s2sx: %v: %s", err, stderr.Bytes())
				}
				if mode != "stdin" {
					if mode == "remove" {
						if _, err := os.Stat(name); !os.IsNotExist(err) {
							t.Errorf("source was not removed: %v", err)
						}
					} else if got, err := os.ReadFile(name); err != nil || !bytes.Equal(got, tt.input) {
						t.Errorf("source = %x, %v, want %x", got, err, tt.input)
					}
					suffix := ".s2sx"
					if destOS == "windows" {
						suffix += ".exe"
					}
					archive, err = os.ReadFile(name + suffix)
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(archive) < len(stub)+1 || !bytes.Equal(archive[:len(stub)], stub) || archive[len(stub)] != opUnpack {
					t.Fatal("archive is missing the unpacker")
				}
				body := archive[len(stub)+1:]
				if tt.compressed && !bytes.Equal(body, tt.input) {
					t.Error("precompressed input was not copied unchanged")
				}
				got, err := io.ReadAll(s2.NewReader(bytes.NewReader(body)))
				if err != nil || !bytes.Equal(got, tt.want) {
					t.Errorf("decoded = %x, %v, want %x", got, err, tt.want)
				}
			})
		}
	}
}
