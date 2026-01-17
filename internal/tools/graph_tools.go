package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/heefoo/codeloom/internal/agent"
	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
)

type GraphTools struct {
	storage   *graph.Storage
	embedding embedding.Provider
}

func NewGraphTools(storage *graph.Storage, embeddingProvider embedding.Provider) *GraphTools {
	return &GraphTools{
		storage:   storage,
		embedding: embeddingProvider,
	}
}

func (g *GraphTools) GetTools() []agent.Tool {
	return []agent.Tool{
		g.semanticSearchTool(),
		g.getTransitiveDependenciesTool(),
		g.traceCallChainTool(),
		g.findByNameTool(),
		g.getNodesByFileTool(),
	}
}

func (g *GraphTools) semanticSearchTool() agent.Tool {
	return agent.Tool{
		Name:        "semantic_search",
		Description: "Search for code using natural language. Returns relevant code nodes based on semantic similarity.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (default: 10)",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			query, ok := args["query"].(string)
			if !ok {
				return "", fmt.Errorf("query parameter must be a string")
			}
			if query = strings.TrimSpace(query); query == "" {
				return "", fmt.Errorf("query parameter cannot be empty")
			}

			limit := 10
			if l, ok := args["limit"].(float64); ok {
				if l < 1 || l > 100 {
					return "", fmt.Errorf("limit must be between 1 and 100, got %f", l)
				}
				limit = int(l)
			}

			// Generate embedding for query
			queryEmbedding, err := g.embedding.EmbedSingle(ctx, query)
			if err != nil {
				return "", fmt.Errorf("embedding error: %w", err)
			}

			// Search in storage
			nodes, err := g.storage.SemanticSearch(ctx, queryEmbedding, limit)
			if err != nil {
				return "", fmt.Errorf("search error: %w", err)
			}

			result := map[string]interface{}{
				"query":   query,
				"count":   len(nodes),
				"results": formatNodes(nodes),
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal semantic search result: %v", err)
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

func (g *GraphTools) getTransitiveDependenciesTool() agent.Tool {
	return agent.Tool{
		Name:        "get_dependencies",
		Description: "Get all transitive dependencies of a code node up to a specified depth.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the node to analyze",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum depth to traverse (default: 3)",
				},
			},
			"required": []string{"node_id"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			nodeID, ok := args["node_id"].(string)
			if !ok {
				return "", fmt.Errorf("node_id parameter must be a string")
			}
			if nodeID = strings.TrimSpace(nodeID); nodeID == "" {
				return "", fmt.Errorf("node_id parameter cannot be empty")
			}

			depth := 3
			if d, ok := args["depth"].(float64); ok {
				if d < 1 || d > 10 {
					return "", fmt.Errorf("depth must be between 1 and 10, got %f", d)
				}
				depth = int(d)
			}

			nodes, err := g.storage.GetTransitiveDependencies(ctx, nodeID, depth)
			if err != nil {
				return "", fmt.Errorf("query error: %w", err)
			}

			result := map[string]interface{}{
				"node_id":      nodeID,
				"depth":        depth,
				"count":        len(nodes),
				"dependencies": formatNodes(nodes),
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal dependencies result: %v", err)
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

func (g *GraphTools) traceCallChainTool() agent.Tool {
	return agent.Tool{
		Name:        "trace_calls",
		Description: "Trace the call chain from one function to another.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"from": map[string]interface{}{
					"type":        "string",
					"description": "Starting function name or ID",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Target function name or ID",
				},
			},
			"required": []string{"from", "to"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			from, ok := args["from"].(string)
			if !ok {
				return "", fmt.Errorf("from parameter must be a string")
			}
			if from = strings.TrimSpace(from); from == "" {
				return "", fmt.Errorf("from parameter cannot be empty")
			}

			to, ok := args["to"].(string)
			if !ok {
				return "", fmt.Errorf("to parameter must be a string")
			}
			if to = strings.TrimSpace(to); to == "" {
				return "", fmt.Errorf("to parameter cannot be empty")
			}

			edges, err := g.storage.TraceCallChain(ctx, from, to)
			if err != nil {
				return "", fmt.Errorf("trace error: %w", err)
			}

			result := map[string]interface{}{
				"from":       from,
				"to":         to,
				"count":      len(edges),
				"call_chain": formatEdges(edges),
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal trace result: %v", err)
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

func (g *GraphTools) findByNameTool() agent.Tool {
	return agent.Tool{
		Name:        "find_by_name",
		Description: "Find code nodes by name (partial match supported).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name to search for",
				},
			},
			"required": []string{"name"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			name, ok := args["name"].(string)
			if !ok {
				return "", fmt.Errorf("name parameter must be a string")
			}
			if name = strings.TrimSpace(name); name == "" {
				return "", fmt.Errorf("name parameter cannot be empty")
			}

			nodes, err := g.storage.FindByName(ctx, name)
			if err != nil {
				return "", fmt.Errorf("find error: %w", err)
			}

			result := map[string]interface{}{
				"name":    name,
				"count":   len(nodes),
				"results": formatNodes(nodes),
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal find result: %v", err)
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

func (g *GraphTools) getNodesByFileTool() agent.Tool {
	return agent.Tool{
		Name:        "get_file_nodes",
		Description: "Get all code nodes (functions, classes, etc.) in a specific file.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file",
				},
			},
			"required": []string{"file_path"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
			filePath, ok := args["file_path"].(string)
			if !ok {
				return "", fmt.Errorf("file_path parameter must be a string")
			}
			if filePath = strings.TrimSpace(filePath); filePath == "" {
				return "", fmt.Errorf("file_path parameter cannot be empty")
			}

			nodes, err := g.storage.GetNodesByFile(ctx, filePath)
			if err != nil {
				return "", fmt.Errorf("query error: %w", err)
			}

			result := map[string]interface{}{
				"file_path": filePath,
				"count":     len(nodes),
				"nodes":     formatNodes(nodes),
			}

			jsonBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("Warning: failed to marshal file nodes result: %v", err)
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonBytes), nil
		},
	}
}

func formatNodes(nodes []graph.CodeNode) []map[string]interface{} {
	result := make([]map[string]interface{}, len(nodes))
	for i, n := range nodes {
		result[i] = map[string]interface{}{
			"id":         n.ID,
			"name":       n.Name,
			"type":       n.NodeType,
			"language":   n.Language,
			"file":       n.FilePath,
			"start_line": n.StartLine,
			"end_line":   n.EndLine,
		}
	}
	return result
}

func formatEdges(edges []graph.CodeEdge) []map[string]interface{} {
	result := make([]map[string]interface{}, len(edges))
	for i, e := range edges {
		result[i] = map[string]interface{}{
			"from": e.FromID,
			"to":   e.ToID,
			"type": e.EdgeType,
		}
	}
	return result
}
