package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

const shredPasses = 3
const shredBufSize = 64 * 1024 // 64 KB

// Make the random source injectable so that we can use a deterministic source in tests.
var randomSource io.Reader = rand.Reader

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <file1> [file2 ...]\n", os.Args[0])
		os.Exit(1)
	}
	exitCode := 0
	for _, path := range os.Args[1:] {
		fmt.Printf("Shredding %s...\n", path)
		if err := Shred(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error shredding %s: %v\n", path, err)
			exitCode = 1
		} else {
			fmt.Fprintf(os.Stdout, "Shredded successfully: %s\n", path)
		}
	}
	os.Exit(exitCode)
}

// Shred:
// - Overwrites the file with random data for shredPasses passes via overwriteFile.
// - Then removes (deletes) the file.
func Shred(path string) error {
	if err := overwriteFile(path, shredPasses); err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// overwriteFile:
// - Retrieves the size of the given file.
// - Overwrites the file from start to end with data from randomSource for the given number of passes.
// - DOES NOT delete the file.
// Shred calls this function and then deletes the file afterwards.
func overwriteFile(path string, passes int) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("shred: %s is not a regular file", path)
	}

	size := info.Size()
	if size < 0 {
		return fmt.Errorf("shred: invalid file size for %s", path)
	}

	// Open the file in write-only mode without truncating it.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	// Ensure the file descriptor is closed even if we return early due to an error.
	defer func() { _ = f.Close() }()

	buf := make([]byte, shredBufSize)

	for pass := 0; pass < passes; pass++ {
		if _, err := f.Seek(0, 0); err != nil {
			return fmt.Errorf("seek %s: %w", path, err)
		}

		var written int64
		for written < size {
			chunk := buf
			remaining := size - written
			if remaining < int64(len(chunk)) {
				chunk = chunk[:remaining]
			}

			// In tests, we override randomSource with a fake reader to make the behavior deterministic.
			if _, err := io.ReadFull(randomSource, chunk); err != nil {
				return fmt.Errorf("fill random (pass %d): %w", pass+1, err)
			}

			n, err := f.Write(chunk)
			if err != nil {
				return fmt.Errorf("write pass %d: %w", pass+1, err)
			}
			written += int64(n)
		}

		if err := f.Sync(); err != nil {
			return fmt.Errorf("sync %s: %w", path, err)
		}
	}

	// Actual close is handled by the deferred function above.
	return nil
}
