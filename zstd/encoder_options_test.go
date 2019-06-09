package zstd

import (
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
