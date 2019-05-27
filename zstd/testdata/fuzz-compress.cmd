REM go get -u github.com/dvyukov/go-fuzz/...
cd ..
del /Q zstd-fuzz.zip
go-fuzz-build -tags=compress github.com/klauspost/compress/zstd
cd testdata
del /Q fuzz-compress\crashers\*.*
del /Q fuzz-compress\suppressions\*.*
go-fuzz -bin=../zstd-fuzz.zip -workdir=fuzz-compress -procs=4
