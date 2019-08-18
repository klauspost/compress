package flate

import "unsafe"

func load32(b []byte, i int) uint32 {
	p := uintptr(unsafe.Pointer(&b[0])) + uintptr(i)
	return *(*uint32)(unsafe.Pointer(p))
}

func load64(b []byte, i int) uint64 {
	p := uintptr(unsafe.Pointer(&b[0])) + uintptr(i)
	return *(*uint64)(unsafe.Pointer(p))
}

func load3232(b []byte, i int32) uint32 {
	p := uintptr(unsafe.Pointer(&b[0])) + uintptr(i)
	return *(*uint32)(unsafe.Pointer(p))
}

func load6432(b []byte, i int32) uint64 {
	p := uintptr(unsafe.Pointer(&b[0])) + uintptr(i)
	return *(*uint64)(unsafe.Pointer(p))
}
