//go:build linux && arm64 && !appengine && !noasm && gc

package zstd

import "testing"

func TestIsNeoverseN1(t *testing.T) {
	tests := []struct {
		midr string
		want bool
	}{
		{"0x00000000413fd0c1\n", true},  // Ampere Altra
		{"0x00000000413fd0c1", true},    // Graviton2
		{"0x00000000411fd401\n", false}, // Neoverse V1
		{"0x00000000410fd4f1\n", false}, // Neoverse V2
		{"0x00000000410fd841\n", false}, // Neoverse V3
		{"0x00000000610f0230\n", false}, // Apple
		{"", false},
		{"garbage", false},
	}
	for _, tt := range tests {
		if got := isNeoverseN1(tt.midr); got != tt.want {
			t.Errorf("isNeoverseN1(%q) = %v, want %v", tt.midr, got, tt.want)
		}
	}
}
