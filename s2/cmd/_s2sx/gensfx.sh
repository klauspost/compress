#!/bin/sh

GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
