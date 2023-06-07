//go:build amd64 && !appengine && !noasm && gc
// +build amd64,!appengine,!noasm,gc

package zstd

// matchLen returns the maximum common prefix length of a and b.
// a must be the shortest of the two.
//
//go:noescape
func matchLen(a, b []byte) int
