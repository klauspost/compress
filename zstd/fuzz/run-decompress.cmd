REM go get -u github.com/dvyukov/go-fuzz/...
cd ..
go-fuzz-build -tags=decompress github.com/klauspost/compress/zstd
cd fuzz
del /Q decompress\crashers\*.*
del /Q decompress\suppressions\*.*
go-fuzz -bin=../zstd-fuzz.zip -workdir=decompress -procs=4
