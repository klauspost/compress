//go:build (!amd64 && !arm64 && (!linux || !arm || !arm.7 || nounsafe)) || !gc || noasm

package zstd

// decode sequences from the stream with the provided history but without dictionary.
// No assembly fast path is available for this configuration.
func (s *sequenceDecs) decodeSyncSimple(hist []byte) (bool, error) {
	return false, nil
}
