// +build !appengine
// +build gc
// +build !noasm

package s2

// emitLiteral has the same semantics as in encode_other.go.
//
//go:noescape
func emitLiteral(dst, lit []byte) int
