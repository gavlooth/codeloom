package indexer

import (
	"context"
	"fmt"
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

// mockEmbeddingProviderWithRetry mocks an embedding provider for testing retry logic
type mockEmbeddingProviderWithRetry struct {
	callCount       int
	failUntil       int // Fail for first N calls
	shouldFail     bool
	failAfterN     int // Fail after N successful calls
	embDimension    int
}

func (m *mockEmbeddingProviderWithRetry) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockEmbeddingProviderWithRetry) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	m.callCount++
	// Check if we should fail this call
	if m.failUntil > 0 && m.callCount <= m.failUntil {
		return nil, fmt.Errorf("mock embedding failure %d", m.callCount)
	}
	if m.shouldFail {
		return nil, fmt.Errorf("mock embedding failure on call %d", m.callCount)
	}
	if m.failAfterN > 0 && m.callCount > m.failAfterN {
		return nil, fmt.Errorf("mock embedding failure after %d calls", m.callCount)
	}
	// Return mock embedding
	dim := m.embDimension
	if dim == 0 {
		dim = 128
	}
	emb := make([]float32, dim)
	for i := range emb {
		emb[i] = float32(i)
	}
	return emb, nil
}

func (m *mockEmbeddingProviderWithRetry) Dimension() int {
	return m.embDimension
}

func (m *mockEmbeddingProviderWithRetry) Name() string {
	return "mock-retry"
}

// TestRetryEmbeddingSuccessOnFirstTry tests that embedding works on first attempt
func TestRetryEmbeddingSuccessOnFirstTry(t *testing.T) {
	ctx := context.Background()
	provider := &mockEmbeddingProviderWithRetry{embDimension: 128}

	emb, err := retryEmbedding(ctx, provider, "test-node", "test content")

	if err != nil {
		t.Errorf("Expected success on first attempt, got error: %v", err)
	}

	if emb == nil {
		t.Error("Expected non-nil embedding")
	}

	if len(emb) != 128 {
		t.Errorf("Expected embedding dimension 128, got %d", len(emb))
	}

	if provider.callCount != 1 {
		t.Errorf("Expected 1 call to provider, got %d", provider.callCount)
	}
}

// TestRetryEmbeddingSuccessAfterRetries tests that embedding succeeds after retries
func TestRetryEmbeddingSuccessAfterRetries(t *testing.T) {
	ctx := context.Background()
	// Fail first 2 calls, succeed on 3rd
	provider := &mockEmbeddingProviderWithRetry{failUntil: 2, embDimension: 128}

	startTime := time.Now()
	emb, err := retryEmbedding(ctx, provider, "test-node", "test content")
	elapsed := time.Since(startTime)

	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}

	if emb == nil {
		t.Error("Expected non-nil embedding")
	}

	if provider.callCount != 3 {
		t.Errorf("Expected 3 calls to provider (2 failures + 1 success), got %d", provider.callCount)
	}

	// Should have waited for backoff (at least 500ms for first retry, 1000ms for second)
	if elapsed < 1400*time.Millisecond {
		t.Errorf("Expected at least 1.4s of backoff time, got %v", elapsed)
	}
}

// TestRetryEmbeddingFailureAfterMaxRetries tests that embedding fails after max retries
func TestRetryEmbeddingFailureAfterMaxRetries(t *testing.T) {
	ctx := context.Background()
	// Always fail
	provider := &mockEmbeddingProviderWithRetry{shouldFail: true, embDimension: 128}

	emb, err := retryEmbedding(ctx, provider, "test-node", "test content")

	if err == nil {
		t.Error("Expected error after max retries, got nil")
	}

	if emb != nil {
		t.Error("Expected nil embedding after max retries")
	}

	if provider.callCount != 3 {
		t.Errorf("Expected 3 attempts (max retries), got %d", provider.callCount)
	}

	// Verify error message contains information about retries
	if !containsString(err.Error(), "after 3 attempts") {
		t.Errorf("Expected error message to contain 'after 3 attempts', got: %s", err.Error())
	}
}

// TestRetryEmbeddingContextCancellation tests that retry respects context cancellation
func TestRetryEmbeddingContextCancellation(t *testing.T) {
	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	provider := &mockEmbeddingProviderWithRetry{embDimension: 128}

	emb, err := retryEmbedding(ctx, provider, "test-node", "test content")

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	if emb != nil {
		t.Error("Expected nil embedding on context cancellation")
	}

	if provider.callCount > 1 {
		t.Errorf("Expected at most 1 call due to cancellation, got %d", provider.callCount)
	}
}

// TestRetryEmbeddingMidRetryCancellation tests that retry respects context cancellation during backoff
func TestRetryEmbeddingMidRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &mockEmbeddingProviderWithRetry{
		failUntil: 1,  // Fail first call
		embDimension: 128,
	}

	// Cancel after short delay (during first backoff)
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	emb, err := retryEmbedding(ctx, provider, "test-node", "test content")

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}

	if emb != nil {
		t.Error("Expected nil embedding on context cancellation")
	}

	// Should have made 1 call (failure) before noticing cancellation
	if provider.callCount != 1 {
		t.Errorf("Expected 1 call before cancellation, got %d", provider.callCount)
	}
}

// Helper function for string contains check
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
