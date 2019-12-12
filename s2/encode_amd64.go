// +build !appengine
// +build gc
// +build !noasm

package s2

// emitLiteral has the same semantics as in encode_go.go.
//
//go:noescape
func emitLiteral(dst, lit []byte) int

// emitCopy has the same semantics as in encode_go.go.
//
//go:noescape
func emitRepeat(dst []byte, offset, length int) int
