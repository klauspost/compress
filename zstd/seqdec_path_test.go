//go:build (amd64 || arm64) && !appengine && !noasm && gc

package zstd

// haveSeqdecAsm reports whether decodeSyncSimple can run the assembly
// decoder on this build, so tests can require that a buffer geometry which
// should select it actually did.
const haveSeqdecAsm = true

// decodeSyncUsesSafe reports which copy variant the assembly decoder would
// use for s's current buffers. It must be asked before decoding, since the
// answer depends on the capacity left in s.out.
func decodeSyncUsesSafe(s *sequenceDecs) bool {
	return s.useSafeDecodeSync()
}
