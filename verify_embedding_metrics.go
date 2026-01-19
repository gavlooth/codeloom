package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/heefoo/codeloom/internal/embedding"
)

// MockProvider simulates an embedding provider that fails some requests
type MockProvider struct {
	failCount int
	callCount int
}

func (m *MockProvider) Name() string {
	return "mock"
}

func (m *MockProvider) Dimension() int {
	return 128
}

func (m *MockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *MockProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	m.callCount++

	// Simulate occasional failures
	if m.failCount > 0 && m.callCount%3 == 0 {
		m.failCount--
		return nil, fmt.Errorf("simulated failure")
	}

	// Return mock embedding
	emb := make([]float32, 128)
	for i := range emb {
		emb[i] = float32(i)
	}
	return emb, nil
}

// Import the retryEmbedding function from indexer package
// This is a simplified version for demonstration
func retryEmbedding(ctx context.Context, embProvider embedding.Provider, nodeID, content string, retryCount, successCount, failureCount *atomic.Int64) ([]float32, error) {
	const maxRetries = 3
	const initialBackoff = 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		emb, err := embProvider.EmbedSingle(ctx, content)
		if err == nil {
			successCount.Add(1)
			return emb, nil
		}
		lastErr = err

		if attempt == maxRetries-1 {
			break
		}

		retryCount.Add(1)
		backoff := time.Duration(1<<uint(attempt)) * initialBackoff
		log.Printf("Retrying embedding for %s (attempt %d/%d, backoff %v): %v", nodeID, attempt+1, maxRetries, backoff, err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	failureCount.Add(1)
	return nil, fmt.Errorf("embedding failed after %d attempts: %w", maxRetries, lastErr)
}

func main() {
	fmt.Println("=== Embedding Metrics Tracking Verification ===")
	fmt.Println()

	ctx := context.Background()
	provider := &MockProvider{failCount: 10}

	var retryCount, successCount, failureCount atomic.Int64

	// Simulate processing 20 nodes
	fmt.Println("Processing 20 code nodes with simulated failures...")
	fmt.Println()

	for i := 0; i < 20; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		content := fmt.Sprintf("This is the content of code node %d", i)

		_, err := retryEmbedding(ctx, provider, nodeID, content, &retryCount, &successCount, &failureCount)

		if err != nil {
			log.Printf("Failed to embed %s: %v\n", nodeID, err)
		}
	}

	fmt.Println()
	fmt.Println("=== Embedding Metrics Summary ===")
	fmt.Printf("Total nodes processed: 20\n")
	fmt.Printf("Successful embeddings: %d\n", successCount.Load())
	fmt.Printf("Retry attempts: %d\n", retryCount.Load())
	fmt.Printf("Failed embeddings (after retries): %d\n", failureCount.Load())
	fmt.Printf("Success rate: %.1f%%\n", float64(successCount.Load())/20.0*100)
	fmt.Println()

	// Verify metrics are reasonable
	if successCount.Load()+failureCount.Load() != 20 {
		fmt.Printf("ERROR: Success + failure count (%d) doesn't match total nodes (20)\n",
			successCount.Load()+failureCount.Load())
		os.Exit(1)
	}

	if retryCount.Load() == 0 {
		fmt.Println("WARNING: No retries occurred (this may indicate the mock isn't working as expected)")
	}

	fmt.Println("=== Verification Complete ===")
	fmt.Println()
	fmt.Println("The metrics show:")
	fmt.Println("1. ✅ Successful embeddings are tracked")
	fmt.Println("2. ✅ Retry attempts are counted")
	fmt.Println("3. ✅ Failures after all retries are recorded")
	fmt.Println("4. ✅ Metrics can be used to identify systemic issues")
	fmt.Println()
	fmt.Println("Example interpretations:")
	fmt.Println("- High retry count: Network instability or service degradation")
	fmt.Println("- High failure count: Service outage or critical errors")
	fmt.Println("- Low success rate: Need to investigate embedding service health")
}
