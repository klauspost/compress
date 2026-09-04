//go:build (!amd64 && !arm64) || appengine || !gc || noasm

package zstd

// No assembly decoder on this build: decodeSyncSimple always declines and
// the Go loop runs. See seqdec_path_test.go.
const haveSeqdecAsm = false

func decodeSyncUsesSafe(s *sequenceDecs) bool {
	return true
}
