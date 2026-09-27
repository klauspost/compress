module github.com/klauspost/compress/huff0/_generate

go 1.25.0

require (
	github.com/klauspost/compress v1.15.15
	github.com/mmcloughlin/avo v0.6.0
)

require (
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
)

replace github.com/klauspost/compress => ../..

replace github.com/mmcloughlin/avo => github.com/honeycombio/avo v0.6.1-0.20260927153959-d123e1d5f255
