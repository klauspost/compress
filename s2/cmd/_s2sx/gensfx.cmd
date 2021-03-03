SET GOOS=linux
SET GOARCH=amd64
go build -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
SET GOOS=darwin
go build -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
SET GOOS=windows
go build -o ./sfx-exe/%GOOS%-%GOARCH% ./_unpack/main.go
