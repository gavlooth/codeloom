package indexer

import (
	"context"
	"testing"
	"time"

	"github.com/heefoo/codeloom/internal/parser"
)

// TestStoreNodesBatchContextCancellation tests that StoreNodesBatch respects context cancellation
// and stops processing batches when context is cancelled.
//
// ISSUE: Before the fix, worker goroutines would continue processing all pending batches
// even after context was cancelled, wasting computational resources and delaying function return.
//
// FIX: Added select statements to check for context cancellation before processing each batch
// in the worker pool and before storing each batch. This allows graceful shutdown.
func TestStoreNodesBatchContextCancellation(t *testing.T) {
	// Create a mock embedding provider that simulates slow embedding
	slowEmbedding := &mockEmbeddingProvider{
		delay: 50 * time.Millisecond,
	}

	// Create temporary storage (in-memory would be ideal, but we'll use nil storage)
	// We're testing cancellation behavior, not actual storage
	// Since we can't easily mock storage, we'll test with nil and expect errors
	// which is acceptable for cancellation testing

	// Create test nodes (enough to trigger batching)
	testNodes := make([]parser.CodeNode, 250) // More than one batch (batchSize = 100)
	for i := 0; i < 250; i++ {
		testNodes[i] = parser.CodeNode{
			ID:      "test::node" + string(rune(i)),
			Name:     "TestNode",
			Language: "go",
			FilePath: "/test/file.go",
			Content:  "Test content for embedding generation",
		}
	}

	// Create a context with a short timeout
	// This should trigger cancellation during embedding generation
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Track start time
	startTime := time.Now()

	// Call StoreNodesBatch with nil storage (will error on store, but that's OK)
	// We're testing that it returns quickly due to context cancellation
	err := StoreNodesBatch(ctx, testNodes, nil, slowEmbedding, nil)

	elapsed := time.Since(startTime)

	// Operation should either:
	// 1. Complete successfully (if fast enough)
	// 2. Return context.Canceled error (if timeout triggered)
	// The important thing is that it doesn't hang or take excessively long
	if err == nil {
		// Successfully completed - this is acceptable
		// Verify it completed within reasonable time
		if elapsed > 5*time.Second {
			t.Errorf("Operation took too long (%v), context cancellation may not be working correctly", elapsed)
		}
	} else if err == context.Canceled || err == context.DeadlineExceeded {
		// Correctly cancelled - this is the expected behavior
		t.Logf("PASS: Context was correctly cancelled, elapsed: %v", elapsed)
	} else {
		// Some other error (like storage error) - log but don't fail
		// We're primarily testing cancellation, not full functionality
		t.Logf("Got non-context error (acceptable): %v", err)
	}

	// Verify operation completes in reasonable time
	// With 250 nodes, batch size 100, and 50ms per batch:
	// - 3 batches would take ~150ms
	// - With context timeout of 100ms, we expect cancellation
	// - Should not wait for all 3 batches (450ms total)
	if elapsed > 300*time.Millisecond {
		t.Errorf("Operation took too long (%v), context cancellation may not be respected", elapsed)
	}
}

// mockEmbeddingProvider is a test helper that simulates embedding with configurable delay
type mockEmbeddingProvider struct {
	delay time.Duration
}

func (m *mockEmbeddingProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.delay):
		// Return a simple embedding
		return []float32{0.1, 0.2, 0.3}, nil
	}
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
			embeddings[i] = []float32{0.1, 0.2, 0.3}
		}
	}
	return embeddings, nil
}

func (m *mockEmbeddingProvider) Name() string {
	return "mock"
}

func (m *mockEmbeddingProvider) Dimension() int {
	return 3
}
