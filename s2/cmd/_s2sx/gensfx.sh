#!/bin/sh

GOOS=linux GOARCH=amd64 go build -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=darwin GOARCH=amd64 go build -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
GOOS=windows GOARCH=amd64 go build -o ./sfx-exe/$GOOS-$GOARCH ./_unpack/main.go
