package graph

import (
	"context"
	"testing"
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
