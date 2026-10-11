// Package castagnoli provides the CRC-32 Castagnoli table shared across the module.
package castagnoli

import (
	"hash/crc32"
	"sync"
)

// Table returns the CRC-32 table for the Castagnoli polynomial.
//
// The table is built on the first call instead of at package
// initialization, so importers that never compute a Castagnoli
// checksum do not allocate it.
var Table = sync.OnceValue(func() *crc32.Table {
	return crc32.MakeTable(crc32.Castagnoli)
})
