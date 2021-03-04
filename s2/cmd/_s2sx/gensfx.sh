#!/bin/sh

go build -o=s2c ../s2c

rm -rf sfx-exe/ || true

GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=linux GOARCH=arm go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=linux GOARCH=ppc64le go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=linux GOARCH=mpis64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go

GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go

GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=windows GOARCH=386 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go

./s2c -rm -slower sfx-exe/*

rm s2c
