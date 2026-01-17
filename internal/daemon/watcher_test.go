package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
)

// TestWatcherEdgeIDFormat verifies that edge IDs generated during file watching
// include edge type to prevent collisions
func TestWatcherEdgeIDFormat(t *testing.T) {
	// Mock parser result with multiple edge types between same nodes
	result := &parser.ParseResult{
		Edges: []parser.CodeEdge{
			{
				FromID:   "file.go::funcA",
				ToID:     "file.go::funcB",
				EdgeType: parser.EdgeTypeCalls,
			},
			{
				FromID:   "file.go::funcA",
				ToID:     "file.go::funcB",
				EdgeType: parser.EdgeTypeUses, // Different type, same nodes
			},
		},
	}

	// Verify edge ID format includes edge type
	// This mirrors what happens in watcher.indexFile function
	ids := make(map[string]bool)
	for _, edge := range result.Edges {
		id := graph.FormatEdgeID(edge.FromID, edge.ToID, graph.EdgeType(edge.EdgeType))
		if ids[id] {
			t.Errorf("Edge ID collision: %s (same nodes, different types should have unique IDs)", id)
		}
		ids[id] = true
	}

	// Verify specific ID format: "FromID->ToID:EdgeType"
	expectedIDs := map[string]bool{
		"file.go::funcA->file.go::funcB:calls": true,
		"file.go::funcA->file.go::funcB:uses": true,
	}

	for id := range ids {
		if !expectedIDs[id] {
			t.Errorf("Unexpected edge ID format: %s\nExpected format: FromID->ToID:EdgeType", id)
		}
	}
}

// TestWatcherStopCleanup verifies that calling Stop() properly cleans up
// watcher goroutines and doesn't cause leaks
func TestWatcherStopCleanup(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "watcher_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file with some code
	testFile := filepath.Join(tmpDir, "test.go")
	testContent := `package main

func foo() string {
	return "hello"
}`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create watcher without storage - we're testing cancellation behavior,
	// not actual indexing
	w, err := NewWatcher(WatcherConfig{
		Parser:     parser.NewParser(),
		DebounceMs: 10, // Fast debounce for testing
	})
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Start watcher in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	watchDone := make(chan struct{})
	go func() {
		w.Watch(ctx, []string{tmpDir})
		close(watchDone)
	}()

	// Wait a bit for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Modify file to trigger indexing
	// Note: This may cause errors since we don't have storage,
	// but that's OK - we're just testing cleanup
	modifiedContent := `package main

func bar() string {
	return "world"
}`
	if err := os.WriteFile(testFile, []byte(modifiedContent), 0644); err != nil {
		t.Logf("Warning: Failed to modify test file: %v", err)
	}

	// Wait a moment for file to be queued
	time.Sleep(50 * time.Millisecond)

	// Cancel() context
	cancel()

	// Stop() watcher explicitly
	w.Stop()

	// Verify that Watch() returns within a reasonable time
	select {
	case <-watchDone:
		// Success - watcher stopped cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("Watcher did not stop within 5 seconds - likely goroutine leak")
	}

	// Give a moment for any goroutines to clean up
	time.Sleep(100 * time.Millisecond)
}

// TestWatcherContextCancellation verifies that indexFile respects context cancellation
func TestWatcherContextCancellation(t *testing.T) {
	// This test verifies that our context checks in indexFile work correctly

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create watcher
	w, err := NewWatcher(WatcherConfig{
		Parser:     parser.NewParser(),
		DebounceMs: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Try to index with cancelled context
	// Should return immediately with context.Canceled error
	err = w.indexFile(ctx, "/fake/path/test.go")

	if err == nil {
		t.Error("indexFile should return error when context is cancelled")
	} else if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}
