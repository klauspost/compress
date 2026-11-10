//go:build arm && !noasm && gc

package zstd

// The arm32 decoder always uses bounds-exact copies; it has no extended-copy
// variant. See seqdec_arm.s.
const haveSeqdecAsm = true

const haveUnsafeAsm = false

func decodeSyncUsesSafe(s *sequenceDecs) bool {
	return true
}
