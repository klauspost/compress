// We enable 64 bit LE platforms:

//go:build (amd64 || arm64 || ppc64le || riscv64) && !nounsafe && !purego && !appengine

package le

import (
	"unsafe"
)

func Load16[I Indexer](b []byte, i I) uint16 {
	//return binary.LittleEndian.Uint16(b[i:])
	//return *(*uint16)(unsafe.Pointer(&b[i]))
	return *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

func Load32[I Indexer](b []byte, i I) uint32 {
	//return binary.LittleEndian.Uint32(b[i:])
	//return *(*uint32)(unsafe.Pointer(&b[i]))
	return *(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

func Load64[I Indexer](b []byte, i I) uint64 {
	//return binary.LittleEndian.Uint64(b[i:])
	//return *(*uint64)(unsafe.Pointer(&b[i]))
	return *(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(i)*unsafe.Sizeof(b[0])))
}

func Store16(b []byte, v uint16) {
	//binary.LittleEndian.PutUint16(b, v)
	*(*uint16)(unsafe.Pointer(&b[0])) = v
}

func Store32(b []byte, v uint32) {
	//binary.LittleEndian.PutUint32(b, v)
	*(*uint32)(unsafe.Pointer(&b[0])) = v
}
