cd ..
go-fuzz-build -tags=compress github.com/klauspost/compress/fse
cd fuzz
go-fuzz -bin=../fse-fuzz.zip -workdir=compress -procs=4
