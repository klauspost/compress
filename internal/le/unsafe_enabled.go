// We enable 64 bit LE platforms:

//go:build (amd64 || arm64 || ppc64le || riscv64) && !nounsafe && !purego && !appengine

package le

import (
	"unsafe"
)

// Load16 will load from b at index i.
// If the compiler can prove that b is at least 1 byte this will be without bounds check.
func Load16[I Indexer](b []byte, i I) uint16 {
	//return binary.LittleEndian.Uint16(b[i:])
	//return *(*uint16)(unsafe.Pointer(&b[i]))
	return *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

// Load32 will load from b at index i.
// If the compiler can prove that b is at least 1 byte this will be without bounds check.
func Load32[I Indexer](b []byte, i I) uint32 {
	//return binary.LittleEndian.Uint32(b[i:])
	//return *(*uint32)(unsafe.Pointer(&b[i]))
	return *(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

// Load64 will load from b at index i.
// If the compiler can prove that b is at least 1 byte this will be without bounds check.
func Load64[I Indexer](b []byte, i I) uint64 {
	//return binary.LittleEndian.Uint64(b[i:])
	//return *(*uint64)(unsafe.Pointer(&b[i]))
	return *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

// Store16 will store v at b.
// If the compiler can prove
func Store16(b []byte, v uint16) {
	//binary.LittleEndian.PutUint16(b, v)
	*(*uint16)(unsafe.Pointer(&b[0])) = v
}

func Store32(b []byte, v uint32) {
	//binary.LittleEndian.PutUint32(b, v)
	*(*uint32)(unsafe.Pointer(&b[0])) = v
}
