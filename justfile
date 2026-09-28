# This project uses just, not make.

set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

default:
    @just --list

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
