#!/bin/bash
set -xe

## Build fuzzing targets
## go-fuzz doesn't support modules for now, so ensure we do everything
## in the old style GOPATH way
export GO111MODULE="off"

## Install go-fuzz
go get -u github.com/dvyukov/go-fuzz/go-fuzz github.com/dvyukov/go-fuzz/go-fuzz-build

## Install fuzzit specific version for production or latest version for development :
# https://github.com/fuzzitdev/fuzzit/releases/latest/download/fuzzit_Linux_x86_64
wget -q -O fuzzit https://github.com/fuzzitdev/fuzzit/releases/download/v2.4.23/fuzzit_Linux_x86_64
chmod a+x fuzzit

go-fuzz-build -libfuzzer -o flate.a github.com/klauspost/compress-fuzz/flate
clang -fsanitize=fuzzer flate.a -o flate-fuzz
./fuzzit create job --type ${1} klauspost/compress-flate flate-fuzz

go-fuzz-build -libfuzzer -o fse-compress.a -func=FuzzCompress github.com/klauspost/compress-fuzz/fse
go-fuzz-build -libfuzzer -o fse-decompress.a -func=FuzzDecompress github.com/klauspost/compress-fuzz/fse
clang -fsanitize=fuzzer fse-compress.a -o fse-compress-fuzz
clang -fsanitize=fuzzer fse-decompress.a -o fse-decompress-fuzz
./fuzzit create job --type ${1} klauspost/compress-fse-compress fse-compress-fuzz
./fuzzit create job --type ${1} klauspost/compress-fse-decompress fse-decompress-fuzz

go-fuzz-build -libfuzzer -o huff0-compress.a -func=FuzzCompress github.com/klauspost/compress-fuzz/huff0
go-fuzz-build -libfuzzer -o huff0-decompress.a -func=FuzzDecompress github.com/klauspost/compress-fuzz/huff0
clang -fsanitize=fuzzer huff0-compress.a -o huff0-compress-fuzz
clang -fsanitize=fuzzer huff0-decompress.a -o huff0-decompress-fuzz
./fuzzit create job --type ${1} klauspost/compress-huff0-compress huff0-compress-fuzz
./fuzzit create job --type ${1} klauspost/compress-huff0-decompress huff0-decompress-fuzz

go-fuzz-build -libfuzzer -o zstd-compress.a -func=FuzzCompress github.com/klauspost/compress-fuzz/zstd
go-fuzz-build -libfuzzer -o zstd-decompress.a -func=FuzzDecompress github.com/klauspost/compress-fuzz/zstd
clang -fsanitize=fuzzer zstd-compress.a -o zstd-compress-fuzz
clang -fsanitize=fuzzer zstd-decompress.a -o zstd-decompress-fuzz
./fuzzit create job --type ${1} klauspost/compress-zstd-compress zstd-compress-fuzz
./fuzzit create job --type ${1} klauspost/compress-zstd-decompress zstd-decompress-fuzz
