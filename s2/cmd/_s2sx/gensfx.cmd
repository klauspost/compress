SET GOOS=linux
SET GOARCH=amd64
go build -ldflags="-s -w" -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
SET GOOS=darwin
go build  -ldflags="-s -w" -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
SET GOOS=windows
go build -ldflags="-s -w" -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
