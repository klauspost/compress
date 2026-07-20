module github.com/klauspost/compress/zstd/_generate

go 1.24

toolchain go1.24.2

require (
	github.com/klauspost/compress v1.15.15
	github.com/mmcloughlin/avo v0.6.1-0.20260709170202-520357590ac6
)

require (
	golang.org/x/mod v0.27.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/tools v0.36.0 // indirect
)

replace github.com/klauspost/compress => ../..

replace github.com/mmcloughlin/avo => github.com/honeycombio/avo v0.6.1-0.20260718041253-a4dbeacc0c55
