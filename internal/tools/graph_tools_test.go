package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/heefoo/codeloom/internal/graph"
)

// mockStorage implements graph.StorageInterface for testing
type mockStorage struct {
	nodes      []graph.CodeNode
	edges      []graph.CodeEdge
	searchFunc func(queryEmbedding []float32, limit int) ([]graph.CodeNode, error)
	depFunc    func(nodeID string, depth int) ([]graph.CodeNode, error)
	traceFunc  func(from, to string) ([]graph.CodeEdge, error)
	nameFunc   func(name string) ([]graph.CodeNode, error)
	fileFunc   func(filePath string) ([]graph.CodeNode, error)
}

func (m *mockStorage) UpsertNode(ctx context.Context, node *graph.CodeNode) error { return nil }
func (m *mockStorage) UpsertEdge(ctx context.Context, edge *graph.CodeEdge) error { return nil }
func (m *mockStorage) UpsertNodesBatch(ctx context.Context, nodes []*graph.CodeNode) error { return nil }
func (m *mockStorage) UpsertEdgesBatch(ctx context.Context, edges []*graph.CodeEdge) error { return nil }
func (m *mockStorage) GetNode(ctx context.Context, id string) (*graph.CodeNode, error) { return nil, nil }
func (m *mockStorage) GetTransitiveDependencies(ctx context.Context, nodeID string, depth int) ([]graph.CodeNode, error) {
	if m.depFunc != nil {
		return m.depFunc(nodeID, depth)
	}
	return m.nodes, nil
}
func (m *mockStorage) TraceCallChain(ctx context.Context, from, to string) ([]graph.CodeEdge, error) {
	if m.traceFunc != nil {
		return m.traceFunc(from, to)
	}
	return m.edges, nil
}
func (m *mockStorage) SemanticSearch(ctx context.Context, queryEmbedding []float32, limit int) ([]graph.CodeNode, error) {
	if m.searchFunc != nil {
		return m.searchFunc(queryEmbedding, limit)
	}
	return m.nodes[:limit], nil
}
func (m *mockStorage) GetAllEdges(ctx context.Context) ([]graph.CodeEdge, error) { return m.edges, nil }
func (m *mockStorage) GetEdgesByType(ctx context.Context, edgeType graph.EdgeType) ([]graph.CodeEdge, error) { return m.edges, nil }
func (m *mockStorage) FindByName(ctx context.Context, name string) ([]graph.CodeNode, error) {
	if m.nameFunc != nil {
		return m.nameFunc(name)
	}
	var matches []graph.CodeNode
	for _, node := range m.nodes {
		if strings.Contains(strings.ToLower(node.Name), strings.ToLower(name)) {
			matches = append(matches, node)
		}
	}
	return matches, nil
}
func (m *mockStorage) GetNodesByFile(ctx context.Context, filePath string) ([]graph.CodeNode, error) {
	if m.fileFunc != nil {
		return m.fileFunc(filePath)
	}
	var fileNodes []graph.CodeNode
	for _, node := range m.nodes {
		if node.FilePath == filePath {
			fileNodes = append(fileNodes, node)
		}
	}
	return fileNodes, nil
}
func (m *mockStorage) GetAllNodes(ctx context.Context) ([]graph.CodeNode, error) { return m.nodes, nil }
func (m *mockStorage) Close() error { return nil }

// mockEmbeddingProvider implements embedding.Provider for testing
type mockEmbeddingProvider struct {
	embedFunc func(ctx context.Context, texts []string) ([][]float32, error)
	singleFunc func(ctx context.Context, text string) ([]float32, error)
	dimension  int
	name       string
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, texts)
	}
	// Return deterministic dummy embeddings
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, 128)
		for j := range result[i] {
			result[i][j] = float32(i + j)
		}
	}
	return result, nil
}

func (m *mockEmbeddingProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	if m.singleFunc != nil {
		return m.singleFunc(ctx, text)
	}
	// Return deterministic dummy embedding
	result := make([]float32, 128)
	for i := range result {
		result[i] = float32(i)
	}
	return result, nil
}

func (m *mockEmbeddingProvider) Dimension() int {
	if m.dimension != 0 {
		return m.dimension
	}
	return 128
}

func (m *mockEmbeddingProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockEmbeddingProvider) Close() error { return nil }

func TestGraphTools_GetTools(t *testing.T) {
	mockStorage := &mockStorage{
		nodes: []graph.CodeNode{
			{ID: "1", Name: "TestFunc", NodeType: "function", Language: "go", FilePath: "test.go", StartLine: 1, EndLine: 10},
		},
		edges: []graph.CodeEdge{
			{ID: "e1", FromID: "1", ToID: "2", EdgeType: "calls", Weight: 1.0},
		},
	}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	allTools := tools.GetTools()

	if len(allTools) != 5 {
		t.Errorf("Expected 5 tools, got %d", len(allTools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range allTools {
		toolNames[tool.Name] = true
		if tool.Name == "" {
			t.Errorf("Tool with empty name found")
		}
		if tool.Description == "" {
			t.Errorf("Tool %s has empty description", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("Tool %s has nil parameters", tool.Name)
		}
		if tool.Execute == nil {
			t.Errorf("Tool %s has nil Execute function", tool.Name)
		}
	}

	expectedTools := []string{"semantic_search", "get_dependencies", "trace_calls", "find_by_name", "get_file_nodes"}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("Expected tool '%s' not found", name)
		}
	}
}

func TestSemanticSearchTool_Success(t *testing.T) {
	mockStorage := &mockStorage{
		searchFunc: func(queryEmbedding []float32, limit int) ([]graph.CodeNode, error) {
			return []graph.CodeNode{
				{ID: "1", Name: "searchResult1", NodeType: "function", Language: "go", FilePath: "file1.go", StartLine: 1, EndLine: 10},
				{ID: "2", Name: "searchResult2", NodeType: "class", Language: "go", FilePath: "file2.go", StartLine: 1, EndLine: 20},
			}, nil
		},
	}
	mockEmbedding := &mockEmbeddingProvider{
		singleFunc: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 128), nil
		},
	}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	searchTool := tools.semanticSearchTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"query": "test query",
		"limit": 10,
	}

	result, err := searchTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Parse JSON result
	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if resultMap["query"] != "test query" {
		t.Errorf("Expected query 'test query', got %v", resultMap["query"])
	}

	if resultMap["count"] != float64(2) {
		t.Errorf("Expected count 2, got %v", resultMap["count"])
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		t.Fatalf("Results field is not an array")
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestSemanticSearchTool_EmptyQuery(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	searchTool := tools.semanticSearchTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"query": "   ", // only whitespace
		"limit": 10,
	}

	_, err := searchTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty query")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestSemanticSearchTool_InvalidLimit(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{
		singleFunc: func(ctx context.Context, text string) ([]float32, error) {
			return make([]float32, 128), nil
		},
	}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	searchTool := tools.semanticSearchTool()

	ctx := context.Background()

	// Test limit too low
	args := map[string]interface{}{
		"query": "test",
		"limit": 0.0,
	}

	_, err := searchTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for limit 0")
	}

	// Test limit too high
	args["limit"] = 101.0
	_, err = searchTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for limit > 100")
	}
}

func TestSemanticSearchTool_EmbeddingError(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{
		singleFunc: func(ctx context.Context, text string) ([]float32, error) {
			return nil, fmt.Errorf("embedding provider unavailable")
		},
	}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	searchTool := tools.semanticSearchTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"query": "test",
	}

	_, err := searchTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for embedding failure")
	}

	if !strings.Contains(err.Error(), "embedding error") {
		t.Errorf("Expected 'embedding error' in error message, got: %v", err)
	}
}

func TestGetTransitiveDependenciesTool_Success(t *testing.T) {
	mockStorage := &mockStorage{
		depFunc: func(nodeID string, depth int) ([]graph.CodeNode, error) {
			return []graph.CodeNode{
				{ID: "2", Name: "dep1", NodeType: "function", Language: "go", FilePath: "file1.go", StartLine: 1, EndLine: 10},
				{ID: "3", Name: "dep2", NodeType: "class", Language: "go", FilePath: "file2.go", StartLine: 1, EndLine: 20},
			}, nil
		},
	}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	depTool := tools.getTransitiveDependenciesTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"node_id": "node1",
		"depth":   3,
	}

	result, err := depTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if resultMap["node_id"] != "node1" {
		t.Errorf("Expected node_id 'node1', got %v", resultMap["node_id"])
	}

	if resultMap["depth"] != float64(3) {
		t.Errorf("Expected depth 3, got %v", resultMap["depth"])
	}

	if resultMap["count"] != float64(2) {
		t.Errorf("Expected count 2, got %v", resultMap["count"])
	}

	dependencies, ok := resultMap["dependencies"].([]interface{})
	if !ok {
		t.Fatalf("Dependencies field is not an array")
	}
	if len(dependencies) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(dependencies))
	}
}

func TestGetTransitiveDependenciesTool_EmptyNodeID(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	depTool := tools.getTransitiveDependenciesTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"node_id": "  ", // only whitespace
		"depth":   3,
	}

	_, err := depTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty node_id")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestGetTransitiveDependenciesTool_InvalidDepth(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	depTool := tools.getTransitiveDependenciesTool()

	ctx := context.Background()

	// Test depth too low
	args := map[string]interface{}{
		"node_id": "node1",
		"depth":   0.0,
	}

	_, err := depTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for depth 0")
	}

	// Test depth too high
	args["depth"] = 11.0
	_, err = depTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for depth > 10")
	}
}

func TestTraceCallChainTool_Success(t *testing.T) {
	mockStorage := &mockStorage{
		traceFunc: func(from, to string) ([]graph.CodeEdge, error) {
			return []graph.CodeEdge{
				{ID: "e1", FromID: "func1", ToID: "func2", EdgeType: "calls", Weight: 1.0},
				{ID: "e2", FromID: "func2", ToID: "func3", EdgeType: "calls", Weight: 1.0},
			}, nil
		},
	}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	traceTool := tools.traceCallChainTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"from": "func1",
		"to":   "func3",
	}

	result, err := traceTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if resultMap["from"] != "func1" {
		t.Errorf("Expected from 'func1', got %v", resultMap["from"])
	}

	if resultMap["to"] != "func3" {
		t.Errorf("Expected to 'func3', got %v", resultMap["to"])
	}

	if resultMap["count"] != float64(2) {
		t.Errorf("Expected count 2, got %v", resultMap["count"])
	}

	callChain, ok := resultMap["call_chain"].([]interface{})
	if !ok {
		t.Fatalf("call_chain field is not an array")
	}
	if len(callChain) != 2 {
		t.Errorf("Expected 2 edges in call chain, got %d", len(callChain))
	}
}

func TestTraceCallChainTool_EmptyFrom(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	traceTool := tools.traceCallChainTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"from": "  ", // only whitespace
		"to":   "func3",
	}

	_, err := traceTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty 'from'")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestTraceCallChainTool_EmptyTo(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	traceTool := tools.traceCallChainTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"from": "func1",
		"to":   "  ", // only whitespace
	}

	_, err := traceTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty 'to'")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestFindByNameTool_Success(t *testing.T) {
	mockStorage := &mockStorage{
		nodes: []graph.CodeNode{
			{ID: "1", Name: "TestFunc", NodeType: "function", Language: "go", FilePath: "test.go", StartLine: 1, EndLine: 10},
			{ID: "2", Name: "TestClass", NodeType: "class", Language: "go", FilePath: "test.go", StartLine: 15, EndLine: 25},
		},
	}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	findTool := tools.findByNameTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"name": "Test",
	}

	result, err := findTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if resultMap["name"] != "Test" {
		t.Errorf("Expected name 'Test', got %v", resultMap["name"])
	}

	if resultMap["count"] != float64(2) {
		t.Errorf("Expected count 2, got %v", resultMap["count"])
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		t.Fatalf("Results field is not an array")
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestFindByNameTool_EmptyName(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	findTool := tools.findByNameTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"name": "  ", // only whitespace
	}

	_, err := findTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty name")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestGetNodesByFileTool_Success(t *testing.T) {
	mockStorage := &mockStorage{
		fileFunc: func(filePath string) ([]graph.CodeNode, error) {
			return []graph.CodeNode{
				{ID: "1", Name: "Func1", NodeType: "function", Language: "go", FilePath: "test.go", StartLine: 1, EndLine: 10},
				{ID: "2", Name: "Func2", NodeType: "function", Language: "go", FilePath: "test.go", StartLine: 15, EndLine: 25},
			}, nil
		},
	}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	fileTool := tools.getNodesByFileTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"file_path": "test.go",
	}

	result, err := fileTool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var resultMap map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resultMap); err != nil {
		t.Fatalf("Failed to parse result JSON: %v", err)
	}

	if resultMap["file_path"] != "test.go" {
		t.Errorf("Expected file_path 'test.go', got %v", resultMap["file_path"])
	}

	if resultMap["count"] != float64(2) {
		t.Errorf("Expected count 2, got %v", resultMap["count"])
	}

	nodes, ok := resultMap["nodes"].([]interface{})
	if !ok {
		t.Fatalf("Nodes field is not an array")
	}
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}
}

func TestGetNodesByFileTool_EmptyFilePath(t *testing.T) {
	mockStorage := &mockStorage{}
	mockEmbedding := &mockEmbeddingProvider{}

	tools := NewGraphTools(mockStorage, mockEmbedding)
	fileTool := tools.getNodesByFileTool()

	ctx := context.Background()
	args := map[string]interface{}{
		"file_path": "  ", // only whitespace
	}

	_, err := fileTool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected error for empty file_path")
	}

	if !strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("Expected 'cannot be empty' error, got: %v", err)
	}
}

func TestFormatNodes(t *testing.T) {
	nodes := []graph.CodeNode{
		{ID: "1", Name: "Func1", NodeType: "function", Language: "go", FilePath: "test.go", StartLine: 1, EndLine: 10},
		{ID: "2", Name: "Class1", NodeType: "class", Language: "go", FilePath: "test.go", StartLine: 15, EndLine: 25},
	}

	formatted := formatNodes(nodes)

	if len(formatted) != 2 {
		t.Errorf("Expected 2 formatted nodes, got %d", len(formatted))
	}

	// Check first node
	node1 := formatted[0]
	if node1["id"] != "1" {
		t.Errorf("Expected id '1', got %v", node1["id"])
	}
	if node1["name"] != "Func1" {
		t.Errorf("Expected name 'Func1', got %v", node1["name"])
	}
	if node1["start_line"] != 1 {
		t.Errorf("Expected start_line 1, got %v", node1["start_line"])
	}
}

func TestFormatEdges(t *testing.T) {
	edges := []graph.CodeEdge{
		{ID: "e1", FromID: "1", ToID: "2", EdgeType: "calls", Weight: 1.0},
		{ID: "e2", FromID: "2", ToID: "3", EdgeType: "calls", Weight: 1.5},
	}

	formatted := formatEdges(edges)

	if len(formatted) != 2 {
		t.Errorf("Expected 2 formatted edges, got %d", len(formatted))
	}

	// Check first edge
	edge1 := formatted[0]
	if edge1["from"] != "1" {
		t.Errorf("Expected from '1', got %v", edge1["from"])
	}
	if edge1["to"] != "2" {
		t.Errorf("Expected to '2', got %v", edge1["to"])
	}
	if fmt.Sprintf("%v", edge1["type"]) != "calls" {
		t.Errorf("Expected type 'calls', got %v", edge1["type"])
	}
}
