//go:build (!amd64 && !arm64 && (!arm || !arm.7 || nounsafe)) || !gc || noasm

package zstd

// No assembly decoder on this build: decodeSyncSimple always declines and
// the Go loop runs. See seqdec_path_test.go.
const haveSeqdecAsm = false

const haveUnsafeAsm = false

func decodeSyncUsesSafe(s *sequenceDecs) bool {
	return true
}
