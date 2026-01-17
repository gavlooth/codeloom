package graph

import (
	"context"
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
