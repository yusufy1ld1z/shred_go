package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---- Helper types / functions ----

// fakeReader: an io.Reader that fills the buffer with the same byte value.
// This makes randomSource deterministic in tests.
type fakeReader struct {
	b byte
}

func (f *fakeReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = f.b
	}
	return len(p), nil
}

// withFakeRandom temporarily overrides randomSource with a fakeReader
// that always returns the given byte, then restores the original source.
func withFakeRandom(b byte, fn func()) {
	prev := randomSource
	randomSource = &fakeReader{b: b}
	defer func() { randomSource = prev }()
	fn()
}

// ---- Table-driven Shred tests ----

func TestShred(t *testing.T) {
	type args struct {
		setup func(t *testing.T, tmpDir string) string
	}

	tests := []struct {
		name       string
		args       args
		wantErr    bool
		wantExists bool // whether the path should still exist after Shred
		skipOnWin  bool // skip on Windows for permission-specific tests
	}{
		{
			name: "RegularFileRemoved",
			args: args{
				setup: func(t *testing.T, tmpDir string) string {
					path := filepath.Join(tmpDir, "file.txt")
					if err := os.WriteFile(path, []byte("secret data"), 0o600); err != nil {
						t.Fatalf("failed to create temp file: %v", err)
					}
					return path
				},
			},
			wantErr:    false,
			wantExists: false,
		},
		{
			name: "EmptyFileRemoved",
			args: args{
				setup: func(t *testing.T, tmpDir string) string {
					path := filepath.Join(tmpDir, "empty.txt")
					if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
						t.Fatalf("failed to create empty file: %v", err)
					}
					return path
				},
			},
			wantErr:    false,
			wantExists: false,
		},
		{
			name: "NonExistingFile",
			args: args{
				setup: func(t *testing.T, tmpDir string) string {
					// Intentionally do NOT create the file
					return filepath.Join(tmpDir, "does-not-exist")
				},
			},
			wantErr:    true,
			wantExists: false,
		},
		{
			name: "DirectoryIsNotRegularFile",
			args: args{
				setup: func(t *testing.T, tmpDir string) string {
					dir := filepath.Join(tmpDir, "subdir")
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatalf("failed to create directory: %v", err)
					}
					return dir
				},
			},
			wantErr:    true,
			wantExists: true, // directory should remain, Shred should fail
		},
		{
			name:      "ReadOnlyFile",
			skipOnWin: true, // Windows permission semantics are different, focus on Unix
			args: args{
				setup: func(t *testing.T, tmpDir string) string {
					path := filepath.Join(tmpDir, "readonly.txt")
					if err := os.WriteFile(path, []byte("cannot write"), 0o600); err != nil {
						t.Fatalf("failed to create temp file: %v", err)
					}
					if err := os.Chmod(path, 0o400); err != nil {
						t.Fatalf("failed to chmod file: %v", err)
					}
					return path
				},
			},
			wantErr:    true, // we expect Shred to fail because it cannot write
			wantExists: true, // file should still exist
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("skipping permission-specific test on Windows")
			}

			tmpDir := t.TempDir()
			path := tt.args.setup(t, tmpDir)

			t.Logf("=== Starting subtest: %q ===", tt.name)
			t.Logf("Path under test: %s", path)
			t.Logf("Expected: wantErr=%v, wantExists=%v", tt.wantErr, tt.wantExists)

			err := Shred(path)

			// Check error expectation
			if tt.wantErr {
				t.Log("Checking: an error is expected from Shred(...)")
				if assert.Error(t, err, "expected error but got nil for path=%s", path) {
					t.Logf("Result: got error as expected: %v", err)
				} else {
					t.Log("Result: ERROR - Shred did not return an error as expected")
				}
			} else {
				t.Log("Checking: no error is expected from Shred(...)")
				if assert.NoError(t, err, "unexpected error for path=%s", path) {
					t.Log("Result: got no error as expected")
				} else {
					t.Logf("Result: ERROR - Shred returned an error: %v", err)
				}
			}

			// Check existence
			_, statErr := os.Stat(path)
			exists := !os.IsNotExist(statErr)
			t.Logf("Filesystem check: exists=%v (statErr=%v)", exists, statErr)

			if tt.wantExists {
				if assert.True(t, exists, "expected path to still exist: %s", path) {
					t.Log("Result: path still exists as expected")
				} else {
					t.Log("Result: ERROR - path was removed but should still exist")
				}
			} else {
				if assert.False(t, exists, "expected path to be removed: %s", path) {
					t.Log("Result: path was removed as expected")
				} else {
					t.Log("Result: ERROR - path still exists but should have been removed")
				}
			}

			t.Logf("=== Finished subtest: %q ===\n", tt.name)
		})
	}
}

// ---- Deterministic random + overwrite tests ----

// This test verifies that overwriteFile uses randomSource and
// actually overwrites the entire file content.
func TestOverwriteDeterministicRandom(t *testing.T) {
	t.Log("Starting TestOverwriteDeterministicRandom: verifying deterministic overwrite using fake random source")

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "data.bin")

	// Create a 4 KB file with initial content.
	original := bytes.Repeat([]byte("X"), 4096)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	t.Logf("Created test file: %s (size=%d bytes)", path, len(original))

	withFakeRandom(0xAA, func() {
		t.Log("Using fake random source that always returns 0xAA")
		err := overwriteFile(path, 1)
		if assert.NoError(t, err, "overwriteFile returned error") {
			t.Log("overwriteFile completed without error")
		}
	})

	data, err := os.ReadFile(path)
	if assert.NoError(t, err, "failed to read overwritten file") {
		t.Logf("Read back overwritten file: %s (size=%d bytes)", path, len(data))
	}
	if assert.Equal(t, len(original), len(data), "file size changed unexpectedly") {
		t.Log("File size remained the same after overwrite, as expected")
	}

	for i, b := range data {
		if b != 0xAA {
			t.Fatalf("byte at index %d = 0x%x, want 0xAA", i, b)
		}
	}
	t.Log("All bytes in the file are 0xAA as expected - deterministic overwrite verified")
}

// ---- Symlink behavior ----

// In this test:
// - we create a target file and a symlink pointing to it
// - we call Shred on the symlink path
// Expected behavior in this implementation:
//   * Shred follows the symlink and overwrites the target file
//   * Shred removes the symlink path
//   * The target file still exists but its content is overwritten
func TestShredSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on Windows (requires special privileges)")
	}

	t.Log("Starting TestShredSymlink: verifying behavior when shredding a symlink")

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target.txt")
	link := filepath.Join(tmpDir, "link.txt")

	original := []byte("highly secret data")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}
	t.Logf("Created target file: %s", target)

	if err := os.Symlink(target, link); err != nil {
		t.Skipf("failed to create symlink (maybe not supported): %v", err)
	}
	t.Logf("Created symlink: %s -> %s", link, target)

	// Use deterministic fake random source
	withFakeRandom(0xBB, func() {
		t.Log("Using fake random source that always returns 0xBB")
		err := Shred(link)
		if assert.NoError(t, err, "Shred(symlink) returned error") {
			t.Log("Shred on symlink completed without error")
		}
	})

	// Symlink should be removed
	_, err := os.Stat(link)
	if assert.True(t, os.IsNotExist(err), "expected symlink to be removed") {
		t.Log("Symlink was removed as expected")
	} else {
		t.Logf("Symlink still exists or stat error is unexpected: %v", err)
	}

	// Target file should still exist
	data, err := os.ReadFile(target)
	if assert.NoError(t, err, "expected target file to still exist") {
		t.Log("Target file still exists after shredding the symlink")
	}

	// Same size but overwritten with 0xBB
	if assert.Equal(t, len(original), len(data), "target size changed unexpectedly") {
		t.Log("Target file size remained unchanged after overwrite")
	}
	for i, b := range data {
		if b != 0xBB {
			t.Fatalf("byte at index %d = 0x%x, want 0xBB", i, b)
		}
	}
	t.Log("All bytes in the target file are 0xBB as expected - overwrite via symlink verified")
}

// ---- Big file test (performance observation) ----

func TestShredBigFile(t *testing.T) {
	// Skip this in short test mode
	if testing.Short() {
		t.Skip("skipping big file test in short mode")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bigfile.bin")

	const size int64 = 10 * 1024 * 1024 // 10 MB (can be increased to 100MB if needed)

	t.Logf("Starting TestShredBigFile: creating a big file of %d bytes at %s", size, path)

	// Create a 10 MB zero-filled file
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create big file: %v", err)
	}
	buf := make([]byte, 1024*1024) // 1 MB buffer
	var written int64
	for written < size {
		n, err := f.Write(buf)
		if err != nil {
			t.Fatalf("failed to write big file: %v", err)
		}
		written += int64(n)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close big file: %v", err)
	}
	t.Logf("Big file created successfully, total written bytes = %d", written)

	start := time.Now()
	err = Shred(path)
	duration := time.Since(start)

	if assert.NoError(t, err, "Shred(big file) returned error") {
		t.Log("Shred(big file) completed without error")
	}
	_, statErr := os.Stat(path)
	if assert.True(t, os.IsNotExist(statErr), "expected big file to be removed") {
		t.Log("Big file was removed as expected after Shred")
	} else {
		t.Logf("Big file still exists or statErr is unexpected: %v", statErr)
	}

	t.Logf("Shredding of %d bytes took %s", size, duration)
	t.Log("Performance observation logged; no strict time assertion is enforced in this test")
}
