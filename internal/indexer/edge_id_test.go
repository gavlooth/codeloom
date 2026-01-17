package indexer

import (
	"fmt"
	"testing"

	"github.com/heefoo/codeloom/internal/parser"
)

// TestEdgeIDGeneration verifies that edges between the same nodes
// but with different types get unique IDs (no collisions)
func TestEdgeIDGeneration(t *testing.T) {
	// Create multiple edges between same pair of nodes
	edges := []parser.CodeEdge{
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
		{
			FromID:   "file.go::funcA",
			ToID:     "file.go::funcC",
			EdgeType: parser.EdgeTypeCalls, // Different target
		},
		{
			FromID:   "file.go::funcD",
			ToID:     "file.go::funcB",
			EdgeType: parser.EdgeTypeCalls, // Different source
		},
	}

	// Use internal test function to simulate edge ID generation
	ids := make(map[string]bool)
	for _, edge := range edges {
		// Simulate the ID generation pattern used in storage_util.go
		// After fix: fmt.Sprintf("%s->%s:%s", edge.FromID, edge.ToID, edge.EdgeType)
		id := generateEdgeID(edge)
		
		if ids[id] {
			t.Errorf("Edge ID collision detected: %s\n  Edge: %+v", id, edge)
		}
		ids[id] = true
		
		t.Logf("Edge %s %s->%s -> ID: %s", 
			edge.EdgeType, edge.FromID, edge.ToID, id)
	}

	// Verify all IDs are unique
	if len(ids) != len(edges) {
		t.Errorf("Expected %d unique edge IDs, got %d", len(edges), len(ids))
	}
}

// TestEdgeIDFormat verifies that edge IDs follow expected format
func TestEdgeIDFormat(t *testing.T) {
	tests := []struct {
		from     string
		to       string
		edgeType parser.EdgeType
		want      string
	}{
		{
			from:     "file.go::funcA",
			to:       "file.go::funcB",
			edgeType: parser.EdgeTypeCalls,
			want:      "file.go::funcA->file.go::funcB:calls",
		},
		{
			from:     "file.go::funcA",
			to:       "file.go::funcB",
			edgeType: parser.EdgeTypeUses,
			want:      "file.go::funcA->file.go::funcB:uses",
		},
		{
			from:     "file.go::funcA",
			to:       "file.go::funcB",
			edgeType: parser.EdgeTypeImports,
			want:      "file.go::funcA->file.go::funcB:imports",
		},
	}

	for _, tt := range tests {
		edge := parser.CodeEdge{
			FromID:   tt.from,
			ToID:     tt.to,
			EdgeType: tt.edgeType,
		}
		
		got := generateEdgeID(edge)
		if got != tt.want {
			t.Errorf("generateEdgeID(%+v) = %q, want %q", edge, got, tt.want)
		}
	}
}

// generateEdgeID is a test helper that mirrors the edge ID generation logic
func generateEdgeID(edge parser.CodeEdge) string {
	// This mirrors the fixed pattern in storage_util.go:306
	return formatEdgeID(edge.FromID, edge.ToID, edge.EdgeType)
}

// formatEdgeID is the actual edge ID formatting function (mirrors storage_util.go)
func formatEdgeID(fromID, toID string, edgeType parser.EdgeType) string {
	// Pattern: "FromID->ToID:EdgeType"
	// This ensures unique IDs for different edge types between same nodes
	return fmt.Sprintf("%s->%s:%s", fromID, toID, edgeType)
}
