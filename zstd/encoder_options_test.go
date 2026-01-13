package zstd

import (
	"bytes"
	"strconv"
	"testing"
)

func TestEncoderLevelFromString(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name  string
		args  args
		want  bool
		want1 EncoderLevel
	}{
		{
			name:  "fastest",
			args:  args{s: "fastest"},
			want:  true,
			want1: SpeedFastest,
		},
		{
			name:  "fastest-upper",
			args:  args{s: "FASTEST"},
			want:  true,
			want1: SpeedFastest,
		},
		{
			name:  "default",
			args:  args{s: "default"},
			want:  true,
			want1: SpeedDefault,
		},
		{
			name:  "default-UPPER",
			args:  args{s: "Default"},
			want:  true,
			want1: SpeedDefault,
		},
		{
			name:  "invalid",
			args:  args{s: "invalid"},
			want:  false,
			want1: SpeedDefault,
		},
		{
			name:  "unknown",
			args:  args{s: "unknown"},
			want:  false,
			want1: SpeedDefault,
		},
		{
			name:  "empty",
			args:  args{s: ""},
			want:  false,
			want1: SpeedDefault,
		},
		{
			name:  "fastest-string",
			args:  args{s: SpeedFastest.String()},
			want:  true,
			want1: SpeedFastest,
		},
		{
			name:  "default-string",
			args:  args{s: SpeedDefault.String()},
			want:  true,
			want1: SpeedDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := EncoderLevelFromString(tt.args.s)
			if got != tt.want {
				t.Errorf("EncoderLevelFromString() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("EncoderLevelFromString() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func TestEncoderLevelFromZstd(t *testing.T) {
	type args struct {
		level int
	}
	tests := []struct {
		name string
		args args
		want EncoderLevel
	}{
		{
			name: "level-1",
			args: args{level: 1},
			want: SpeedFastest,
		},
		{
			name: "level-minus1",
			args: args{level: -1},
			want: SpeedFastest,
		},
		{
			name: "level-3",
			args: args{level: 3},
			want: SpeedDefault,
		},
		{
			name: "level-4",
			args: args{level: 4},
			want: SpeedDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EncoderLevelFromZstd(tt.args.level); got != tt.want {
				t.Errorf("EncoderLevelFromZstd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindowSize(t *testing.T) {
	tests := []struct {
		windowSize int
		err        bool
	}{
		{1 << 9, true},
		{1 << 10, false},
		{(1 << 10) + 1, true},
		{(1 << 10) * 3, true},
		{MaxWindowSize, false},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.windowSize), func(t *testing.T) {
			var options encoderOptions
			err := WithWindowSize(tt.windowSize)(&options)
			if tt.err {
				if err == nil {
					t.Error("did not get error for invalid window size")
				}
			} else {
				if err != nil {
					t.Error("got error for valid window size")
				}
				if options.windowSize != tt.windowSize {
					t.Error("failed to set window size")
				}
			}
		})
	}
}

func TestEncoderResetWithOptions(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewWriter(&buf, WithEncoderLevel(SpeedFastest), WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	// Test changing safe options
	t.Run("change-crc", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithEncoderCRC(false))
		if err != nil {
			t.Errorf("ResetWithOptions should allow changing CRC: %v", err)
		}
	})

	t.Run("change-padding", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithEncoderPadding(64))
		if err != nil {
			t.Errorf("ResetWithOptions should allow changing padding: %v", err)
		}
	})

	// Test error when changing unsafe options
	t.Run("change-level-error", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithEncoderLevel(SpeedBestCompression))
		if err == nil {
			t.Error("ResetWithOptions should error when changing level")
		}
	})

	t.Run("change-concurrency-error", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithEncoderConcurrency(99))
		if err == nil {
			t.Error("ResetWithOptions should error when changing concurrency")
		}
	})

	t.Run("change-window-error", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithWindowSize(1<<15))
		if err == nil {
			t.Error("ResetWithOptions should error when changing window size")
		}
	})

	t.Run("change-lowmem-error", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithLowerEncoderMem(true))
		if err == nil {
			t.Error("ResetWithOptions should error when changing lowMem")
		}
	})

	// Test same value is allowed
	t.Run("same-level-ok", func(t *testing.T) {
		err := enc.ResetWithOptions(&buf, WithEncoderLevel(SpeedFastest))
		if err != nil {
			t.Errorf("ResetWithOptions should allow same level: %v", err)
		}
	})
}

func TestEncoderDictDelete(t *testing.T) {
	var buf bytes.Buffer
	dictContent := []byte("test dictionary content for compression")
	enc, err := NewWriter(&buf, WithEncoderDictRaw(123, dictContent))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()

	if enc.o.dict == nil {
		t.Fatal("dict should be set")
	}

	err = enc.ResetWithOptions(&buf, WithEncoderDictDelete())
	if err != nil {
		t.Fatal(err)
	}
	if enc.o.dict != nil {
		t.Error("dict should be nil after delete")
	}
}
