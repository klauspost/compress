cd ..
go-fuzz-build -tags=compress github.com/klauspost/compress/huff0
cd fuzz
go-fuzz -bin=../huff0-fuzz.zip -workdir=compress -procs=4
