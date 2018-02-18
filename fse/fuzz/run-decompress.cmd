cd ..
go-fuzz-build -tags=decompress github.com/klauspost/compress/fse
cd fuzz
go-fuzz -bin=../fse-fuzz.zip -workdir=decompress -procs=4
