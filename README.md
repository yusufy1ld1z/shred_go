# Shred (Go)

## Requirements
- Install Go (golang) on your system.

## Build
go build -o goshred

## Usage
./goshred <file1> [file2 ...]

# Run tests (recommended: this project is validated via tests)
go test -v 

## strace (save syscall evidence to a file)
strace go test -v > strace_output.txt 2>&1
