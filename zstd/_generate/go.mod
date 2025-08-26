module github.com/klauspost/compress/s2/_generate

go 1.23

toolchain go1.24.2

require (
	github.com/klauspost/compress v1.15.15
	github.com/mmcloughlin/avo v0.6.0
)

require (
	golang.org/x/mod v0.21.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/tools v0.25.0 // indirect
)

replace github.com/klauspost/compress => ../..
