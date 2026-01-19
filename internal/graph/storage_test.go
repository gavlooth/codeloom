package graph

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestUpdateFileAtomicEmpty tests that UpdateFileAtomic handles empty nodes/edges atomically
// This test verifies the fix for the data integrity issue where separate delete
// operations could leave orphaned data
func TestUpdateFileAtomicEmpty(t *testing.T) {
	// This test requires a running SurrealDB instance
	// Skip in CI environments without database
	t.Skip("requires SurrealDB instance")

	ctx := context.Background()

	// Create a test storage instance
	storage, err := NewStorage(StorageConfig{
		URL:       "ws://localhost:8000/rpc",
		Namespace: "test",
		Database:  "test",
	})
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	// Run migrations
	if err := storage.RunMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create a test file with nodes and edges
	testFile := "/test/file.go"
	testNode := &CodeNode{
		ID:       "test_node_1",
		Name:     "TestNode",
		NodeType: NodeTypeFunction,
		Language: "go",
		FilePath: testFile,
		Content:  "func test() {}",
	}

	testEdge := &CodeEdge{
		ID:       "test_node_1->test_node_2",
		FromID:   "test_node_1",
		ToID:     "test_node_2",
		EdgeType: EdgeTypeCalls,
		Weight:   1.0,
	}

	// Store initial data
	if err := storage.UpsertNode(ctx, testNode); err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	if err := storage.UpsertEdge(ctx, testEdge); err != nil {
		t.Fatalf("failed to create test edge: %v", err)
	}

	// Verify data exists
	nodes, err := storage.GetNodesByFile(ctx, testFile)
	if err != nil {
		t.Fatalf("failed to get nodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Error("expected node to exist before deletion")
	}

	// Call UpdateFileAtomic with empty nodes and edges
	// Before the fix, this would call DeleteEdgesByFile and DeleteNodesByFile separately
	// If DeleteNodesByFile failed, edges would be orphaned
	// After the fix, both deletions happen in a single transaction
	err = storage.UpdateFileAtomic(ctx, testFile, []*CodeNode{}, []*CodeEdge{})
	if err != nil {
		t.Fatalf("UpdateFileAtomic failed: %v", err)
	}

	// Verify all data is deleted (no orphaned data)
	nodes, err = storage.GetNodesByFile(ctx, testFile)
	if err != nil {
		t.Fatalf("failed to get nodes after deletion: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected no nodes after deletion, got %d", len(nodes))
	}

	// Check for orphaned edges (edges referencing deleted nodes)
	edges, err := storage.GetAllEdges(ctx)
	if err != nil {
		t.Fatalf("failed to get edges: %v", err)
	}
	for _, edge := range edges {
		if edge.FromID == "test_node_1" || edge.ToID == "test_node_1" {
			t.Errorf("found orphaned edge: %+v", edge)
		}
	}
}

// TestFileLockingConcurrency verifies that the file locking mechanism works correctly
// under concurrent access and doesn't cause race conditions or deadlocks
func TestFileLockingConcurrency(t *testing.T) {
	storage := &Storage{}

	// Test multiple concurrent lock/unlock operations on the same file
	filePath := "/test/concurrent.go"
	numGoroutines := 100
	operationsPerGoroutine := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines that lock/unlock the same file
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				storage.lockFile(filePath)
				// Simulate some work while holding the lock
				time.Sleep(1 * time.Microsecond)
				storage.unlockFile(filePath)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify that all locks were properly released by checking the fileLocks map
	// After all operations, the entry should be deleted (count should be 0)
	storage.fileLocksMu.Lock()
	defer storage.fileLocksMu.Unlock()

	if fl, exists := storage.fileLocks[filePath]; exists {
		t.Errorf("file lock still exists after all operations: count=%d", fl.count)
	}

	t.Logf("Successfully completed %d concurrent lock/unlock operations on %s",
		numGoroutines*operationsPerGoroutine, filePath)
}

// TestFileLockingMultipleFiles verifies that file locking works correctly
// when locking/unlocking multiple different files concurrently
func TestFileLockingMultipleFiles(t *testing.T) {
	storage := &Storage{}

	numFiles := 10
	numGoroutines := 50
	var filePaths []string
	for i := 0; i < numFiles; i++ {
		filePaths = append(filePaths, t.TempDir()+"/file"+string(rune('0'+i))+".go")
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Each goroutine picks a random file and locks/unlocks it
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Lock/unlock each file once
			for _, filePath := range filePaths {
				storage.lockFile(filePath)
				time.Sleep(1 * time.Microsecond)
				storage.unlockFile(filePath)
			}
		}(i)
	}

	wg.Wait()

	// Verify all locks were properly released
	storage.fileLocksMu.Lock()
	defer storage.fileLocksMu.Unlock()

	if len(storage.fileLocks) != 0 {
		t.Errorf("expected all file locks to be released, but %d entries remain: %v",
			len(storage.fileLocks), storage.fileLocks)
	}

	t.Logf("Successfully completed concurrent locking of %d files", numFiles*numGoroutines)
}

// TestFileLockingRaceCondition specifically tests the race condition scenario
// that was fixed: unlocking a file while another goroutine tries to lock it
func TestFileLockingRaceCondition(t *testing.T) {
	storage := &Storage{}

	filePath := "/test/race.go"

	// Goroutine 1: Lock and hold the file
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		storage.lockFile(filePath)
		time.Sleep(10 * time.Millisecond) // Hold lock for a bit
		storage.unlockFile(filePath)
	}()

	// Goroutine 2: Try to lock the same file while it's being unlocked
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // Wait slightly for goroutine 1 to hold lock
		storage.lockFile(filePath)
		storage.unlockFile(filePath)
	}()

	wg.Wait()

	// Verify no orphaned locks remain
	storage.fileLocksMu.Lock()
	defer storage.fileLocksMu.Unlock()

	if fl, exists := storage.fileLocks[filePath]; exists {
		t.Errorf("race condition test failed: file lock still exists with count=%d", fl.count)
	}

	t.Log("Race condition test passed: no orphaned locks detected")
}

// TestSemanticSearchContextCancellation verifies that SemanticSearch respects context cancellation
// This test verifies fix for CPU-intensive operations that should be cancellable
func TestSemanticSearchContextCancellation(t *testing.T) {
	// This test requires a running SurrealDB instance
	// Skip in CI environments without database
	t.Skip("requires SurrealDB instance")

	ctx := context.Background()

	// Create a test storage instance
	storage, err := NewStorage(StorageConfig{
		URL:       "ws://localhost:8000/rpc",
		Namespace: "test",
		Database:  "test",
	})
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	// Run migrations
	if err := storage.RunMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create many nodes with embeddings to test CPU-intensive loop
	numNodes := 100
	embedding := make([]float32, 128)
	for i := range embedding {
		embedding[i] = float32(i)
	}

	for i := 0; i < numNodes; i++ {
		node := &CodeNode{
			ID:       fmt.Sprintf("test_node_%d", i),
			Name:     fmt.Sprintf("TestNode%d", i),
			NodeType: NodeTypeFunction,
			Language: "go",
			FilePath: fmt.Sprintf("/test/file%d.go", i%10),
			Content:  "func test() {}",
			Embedding: embedding,
		}
		if err := storage.UpsertNode(ctx, node); err != nil {
			t.Fatalf("failed to create test node %d: %v", i, err)
		}
	}

	// Create a context that's already cancelled
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Measure time taken - should return quickly with cancellation error
	start := time.Now()
	nodes, err := storage.SemanticSearch(cancelledCtx, embedding, 10)
	duration := time.Since(start)

	// Verify cancellation error
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	} else if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Verify fast return (should not process all 100 nodes)
	if duration > 500*time.Millisecond {
		t.Errorf("expected quick cancellation (< 500ms), took %v", duration)
	}

	// Verify no results returned
	if len(nodes) != 0 {
		t.Errorf("expected no results from cancelled context, got %d", len(nodes))
	}

	t.Logf("Context cancellation test passed: SemanticSearch cancelled in %v", duration)
}

// TestGetTransitiveDependenciesContextCancellation verifies that GetTransitiveDependencies respects context cancellation
// This test verifies fix for graph traversal operations that should be cancellable
func TestGetTransitiveDependenciesContextCancellation(t *testing.T) {
	// This test requires a running SurrealDB instance
	// Skip in CI environments without database
	t.Skip("requires SurrealDB instance")

	ctx := context.Background()

	// Create a test storage instance
	storage, err := NewStorage(StorageConfig{
		URL:       "ws://localhost:8000/rpc",
		Namespace: "test",
		Database:  "test",
	})
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	// Run migrations
	if err := storage.RunMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create a deep dependency graph (depth 5) to test BFS traversal
	baseNode := &CodeNode{
		ID:       "base_node",
		Name:     "BaseNode",
		NodeType: NodeTypeFunction,
		Language: "go",
		FilePath: "/test/base.go",
		Content:  "func base() {}",
	}
	if err := storage.UpsertNode(ctx, baseNode); err != nil {
		t.Fatalf("failed to create base node: %v", err)
	}

	// Create 5 levels of dependencies
	prevNodeID := "base_node"
	for level := 1; level <= 5; level++ {
		node := &CodeNode{
			ID:       fmt.Sprintf("dep_node_level_%d", level),
			Name:     fmt.Sprintf("DepNodeLevel%d", level),
			NodeType: NodeTypeFunction,
			Language: "go",
			FilePath: "/test/dep.go",
			Content:  "func dep() {}",
		}
		if err := storage.UpsertNode(ctx, node); err != nil {
			t.Fatalf("failed to create dep node level %d: %v", level, err)
		}

		edge := &CodeEdge{
			ID:       FormatEdgeID(prevNodeID, node.ID, EdgeTypeImports),
			FromID:   prevNodeID,
			ToID:     node.ID,
			EdgeType: EdgeTypeImports,
			Weight:   1.0,
		}
		if err := storage.UpsertEdge(ctx, edge); err != nil {
			t.Fatalf("failed to create edge level %d: %v", level, err)
		}

		prevNodeID = node.ID
	}

	// Create a context that's already cancelled
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Measure time taken - should return quickly with cancellation error
	start := time.Now()
	nodes, err := storage.GetTransitiveDependencies(cancelledCtx, "base_node", 10)
	duration := time.Since(start)

	// Verify cancellation error
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	} else if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Verify fast return (should not traverse full graph)
	if duration > 500*time.Millisecond {
		t.Errorf("expected quick cancellation (< 500ms), took %v", duration)
	}

	// Verify no results returned
	if len(nodes) != 0 {
		t.Errorf("expected no results from cancelled context, got %d", len(nodes))
	}

	t.Logf("Context cancellation test passed: GetTransitiveDependencies cancelled in %v", duration)
}

// TestTraceCallChainContextCancellation verifies that TraceCallChain respects context cancellation
// This test verifies fix for graph traversal operations that should be cancellable
func TestTraceCallChainContextCancellation(t *testing.T) {
	// This test requires a running SurrealDB instance
	// Skip in CI environments without database
	t.Skip("requires SurrealDB instance")

	ctx := context.Background()

	// Create a test storage instance
	storage, err := NewStorage(StorageConfig{
		URL:       "ws://localhost:8000/rpc",
		Namespace: "test",
		Database:  "test",
	})
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	// Run migrations
	if err := storage.RunMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create a call chain (A -> B -> C -> D -> E -> F)
	callNodes := []string{"func_a", "func_b", "func_c", "func_d", "func_e", "func_f"}
	for _, name := range callNodes {
		node := &CodeNode{
			ID:       name,
			Name:     name,
			NodeType: NodeTypeFunction,
			Language: "go",
			FilePath: "/test/calls.go",
			Content:  fmt.Sprintf("func %s() {}", name),
		}
		if err := storage.UpsertNode(ctx, node); err != nil {
			t.Fatalf("failed to create node %s: %v", name, err)
		}
	}

	// Create call edges
	for i := 0; i < len(callNodes)-1; i++ {
		edge := &CodeEdge{
			ID:       FormatEdgeID(callNodes[i], callNodes[i+1], EdgeTypeCalls),
			FromID:   callNodes[i],
			ToID:     callNodes[i+1],
			EdgeType: EdgeTypeCalls,
			Weight:   1.0,
		}
		if err := storage.UpsertEdge(ctx, edge); err != nil {
			t.Fatalf("failed to create call edge %s->%s: %v", callNodes[i], callNodes[i+1], err)
		}
	}

	// Create a context that's already cancelled
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Measure time taken - should return quickly with cancellation error
	start := time.Now()
	edges, err := storage.TraceCallChain(cancelledCtx, "func_a", "func_f")
	duration := time.Since(start)

	// Verify cancellation error
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	} else if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Verify fast return (should not traverse full chain)
	if duration > 500*time.Millisecond {
		t.Errorf("expected quick cancellation (< 500ms), took %v", duration)
	}

	// Verify no results returned
	if len(edges) != 0 {
		t.Errorf("expected no results from cancelled context, got %d", len(edges))
	}

	t.Logf("Context cancellation test passed: TraceCallChain cancelled in %v", duration)
}
