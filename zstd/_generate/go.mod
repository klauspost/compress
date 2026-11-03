module github.com/klauspost/compress/zstd/_generate

go 1.25.0

require github.com/klauspost/compress v1.15.15

require (
	github.com/honeycombio/avo v0.6.1-0.20261101015103-01944999a5ad
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
)

replace github.com/klauspost/compress => ../..
