//go:build !(amd64 || arm64 || ppc64le || riscv64) || nounsafe || purego || appengine

package le

import (
	"encoding/binary"
)

func Load16[I Indexer](b []byte, i I) uint16 {
	return binary.LittleEndian.Uint16(b[i:])
}

func Load32[I Indexer](b []byte, i I) uint32 {
	return binary.LittleEndian.Uint32(b[i:])
}

func Load64[I Indexer](b []byte, i I) uint64 {
	return binary.LittleEndian.Uint64(b[i:])
}

func Store16(b []byte, v uint16) {
	binary.LittleEndian.PutUint16(b, v)
}

func Store32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
}

func Store64(b []byte, v uint64) {
	binary.LittleEndian.PutUint64(b, v)
}
