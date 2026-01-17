package daemon

import (
	"testing"

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
	// This mirrors what happens in indexFile function
	ids := make(map[string]bool)
	for _, edge := range result.Edges {
		id := formatEdgeID(edge.FromID, edge.ToID, string(edge.EdgeType))
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
