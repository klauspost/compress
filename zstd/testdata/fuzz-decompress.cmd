REM go get -u github.com/dvyukov/go-fuzz/...
cd ..
del /Q zstd-fuzz.zip
go-fuzz-build -tags=decompress github.com/klauspost/compress/zstd
cd testdata
del /Q fuzz-decompress\crashers\*.*
del /Q fuzz-decompress\suppressions\*.*
go-fuzz -bin=../zstd-fuzz.zip -workdir=fuzz-decompress -procs=4
