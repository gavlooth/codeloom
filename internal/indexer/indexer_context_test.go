package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestComputeFileHashContextCancellation tests that computeFileHash respects context cancellation
func TestComputeFileHashContextCancellation(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a large test file (enough that hashing takes measurable time)
	testFile := filepath.Join(tmpDir, "large_test_file.txt")
	data := make([]byte, 10*1024*1024) // 10MB file
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a context that will be cancelled immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Attempt to hash of file - should return error due to cancelled context
	hash, err := computeFileHash(ctx, testFile)

	if err == nil {
		t.Error("Expected error due to cancelled context, got nil")
	}

	if hash != "" {
		t.Errorf("Expected empty hash due to cancelled context, got %s", hash)
	}

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// TestComputeFileHashNormalOperation tests that computeFileHash works correctly with active context
func TestComputeFileHashNormalOperation(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test_file.txt")
	testContent := "Hello, World! This is a test file."
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a normal context with sufficient time
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Hash of file
	hash, err := computeFileHash(ctx, testFile)

	if err != nil {
		t.Fatalf("Unexpected error computing hash: %v", err)
	}

	if hash == "" {
		t.Error("Expected non-empty hash, got empty string")
	}

	// Expected SHA256 hash of test content
	expectedHash := "1f06cdbe60d64c0325e89b1716e89cf7901c6e7ddac4d265b0b1f2c45c0af20c"
	if hash != expectedHash {
		t.Errorf("Hash mismatch: got %s, want %s", hash, expectedHash)
	}
}

// TestComputeFileHashCancellationDuringHash tests that computeFileHash can be cancelled mid-operation
func TestComputeFileHashCancellationDuringHash(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a large test file
	testFile := filepath.Join(tmpDir, "very_large_test_file.txt")
	data := make([]byte, 50*1024*1024) // 50MB file
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a context with a very short timeout (but long enough to open file)
	// The timeout should be short enough to cancel before large file completes
	// but long enough to verify context checking works
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Attempt to hash of file
	startTime := time.Now()
	hash, err := computeFileHash(ctx, testFile)
	elapsed := time.Since(startTime)

	// Operation should either:
	// 1. Complete successfully (if timeout is long enough)
	// 2. Return context.Canceled error (if timeout triggers mid-operation)
	// The important thing is that it doesn't hang indefinitely
	if err == nil {
		// Successfully hashed - verify it's not empty
		if hash == "" {
			t.Error("Expected non-empty hash on successful completion")
		}
		// With 50MB file, 50ms timeout should likely trigger cancellation
		// If it completes, that's acceptable too
	} else if err == context.Canceled {
		// Correctly cancelled - hash should be empty
		if hash != "" {
			t.Errorf("Expected empty hash on cancellation, got %s", hash)
		}
	} else {
		// Some other error - report it
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify operation completed within reasonable time
	// A successful hash of 50MB would take much longer than 50ms
	// So we expect either cancellation or quick error
	if elapsed > 200*time.Millisecond {
		t.Errorf("Operation took too long (%v), context may not be properly respected", elapsed)
	}
}
