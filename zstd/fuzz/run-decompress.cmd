REM go get -u github.com/dvyukov/go-fuzz/...
cd ..
go-fuzz-build -tags=decompress github.com/klauspost/compress/zstd
cd fuzz
go-fuzz -bin=../zstd-fuzz.zip -workdir=decompress -procs=4
