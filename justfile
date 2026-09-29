# This project uses just, not make.

set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

default:
    @just --list

# Run the program; pass flags after, e.g. `just run -port 7400`
run *flags:
    go run ./cmd/tierlist {{ flags }}

# Run the program without opening a browser, to refresh a tab already open
serve *flags:
    go run ./cmd/tierlist -no-browser {{ flags }}

# Build the program into bin/
build:
    go build -o bin/ ./cmd/tierlist

# Run the tests
test:
    go test ./...

# Report suspicious code with go vet
vet:
    go vet ./...

# Format the Go code
fmt:
    go fmt ./...

# Run go vet and the tests
check: vet test
