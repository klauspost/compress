cd ..
go-fuzz-build -tags=decompress github.com/klauspost/compress/huff0
cd fuzz
go-fuzz -bin=../huff0-fuzz.zip -workdir=decompress -procs=4
