//go:build zstdlegacycost

package zstd

// legacyCost keeps the table cost estimate of v1.20.2 for frames without
// a dictionary, so their output matches that version.
const legacyCost = true
