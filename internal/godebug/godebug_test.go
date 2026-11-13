//go:debug zipinsecurepath=0

package godebug

import "testing"

// The directive above is recorded in this test binary's default GODEBUG,
// like a //go:debug directive in package main or a godebug line in go.mod.
func TestGetDefault(t *testing.T) {
	for _, tt := range []struct {
		env, want string
	}{
		{env: "", want: "0"},
		{env: "x=1", want: "0"},
		{env: "zipinsecurepath=1", want: "1"},
		{env: "zipinsecurepath=", want: ""},
	} {
		t.Setenv("GODEBUG", tt.env)
		if got := Get("zipinsecurepath"); got != tt.want {
			t.Errorf("GODEBUG=%q: got %q, want %q", tt.env, got, tt.want)
		}
	}
}
