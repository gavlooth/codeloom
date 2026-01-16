package graph

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/surrealdb/surrealdb.go"
)

type Storage struct {
	db        *surrealdb.DB
	namespace string
	database  string
}

type StorageConfig struct {
	URL       string
	Namespace string
	Database  string
	Username  string
	Password  string
}

type NodeType string

const (
	NodeTypeFunction  NodeType = "function"
	NodeTypeClass     NodeType = "class"
	NodeTypeModule    NodeType = "module"
	NodeTypeVariable  NodeType = "variable"
	NodeTypeType      NodeType = "type"
	NodeTypeInterface NodeType = "interface"
	NodeTypeStruct    NodeType = "struct"
	NodeTypeEnum      NodeType = "enum"
	NodeTypeMethod    NodeType = "method"
)

type EdgeType string

const (
	EdgeTypeCalls      EdgeType = "calls"
	EdgeTypeImports    EdgeType = "imports"
	EdgeTypeUses       EdgeType = "uses"
	EdgeTypeExtends    EdgeType = "extends"
	EdgeTypeImplements EdgeType = "implements"
	EdgeTypeReferences EdgeType = "references"
)

type CodeNode struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	NodeType    NodeType          `json:"node_type,omitempty"`
	Language    string            `json:"language"`
	FilePath    string            `json:"file_path"`
	StartLine   int               `json:"start_line"`
	EndLine     int               `json:"end_line"`
	Content     string            `json:"content,omitempty"`
	DocComment  string            `json:"doc_comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Embedding   []float32         `json:"embedding,omitempty"`
	Complexity  float32           `json:"complexity,omitempty"`
}

type CodeEdge struct {
	ID       string   `json:"id"`
	FromID   string   `json:"from_id"`
	ToID     string   `json:"to_id"`
	EdgeType EdgeType `json:"edge_type"`
	Weight   float32  `json:"weight"`
}

// ScoredNode represents a node with its similarity score
type ScoredNode struct {
	Node  CodeNode
	Score float64
}

func NewStorage(cfg StorageConfig) (*Storage, error) {
	ctx := context.Background()
	db, err := surrealdb.New(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SurrealDB: %w", err)
	}

	// Sign in
	if cfg.Username != "" {
		_, err = db.SignIn(ctx, map[string]interface{}{
			"user": cfg.Username,
			"pass": cfg.Password,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to sign in: %w", err)
		}
	}

	// Use namespace and database
	err = db.Use(ctx, cfg.Namespace, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to use namespace/database: %w", err)
	}

	return &Storage{
		db:        db,
		namespace: cfg.Namespace,
		database:  cfg.Database,
	}, nil
}

func (s *Storage) Close() error {
	return s.db.Close(context.Background())
}

func (s *Storage) UpsertNode(ctx context.Context, node *CodeNode) error {
	// Use UPSERT to create or update the node
	query := `UPSERT nodes SET
		id = $id,
		name = $name,
		node_type = $node_type,
		language = $language,
		file_path = $file_path,
		start_line = $start_line,
		end_line = $end_line,
		content = $content,
		doc_comment = $doc_comment,
		annotations = $annotations,
		embedding = $embedding,
		complexity = $complexity
	WHERE id = $id`

	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"id":          node.ID,
		"name":        node.Name,
		"node_type":   string(node.NodeType),
		"language":    node.Language,
		"file_path":   node.FilePath,
		"start_line":  node.StartLine,
		"end_line":    node.EndLine,
		"content":     node.Content,
		"doc_comment": node.DocComment,
		"annotations": node.Annotations,
		"embedding":   node.Embedding,
		"complexity":  node.Complexity,
	})
	return err
}

func (s *Storage) UpsertEdge(ctx context.Context, edge *CodeEdge) error {
	// Use UPSERT to create or update the edge
	query := `UPSERT edges SET
		id = $id,
		from_id = $from_id,
		to_id = $to_id,
		edge_type = $edge_type,
		weight = $weight
	WHERE id = $id`

	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"id":        edge.ID,
		"from_id":   edge.FromID,
		"to_id":     edge.ToID,
		"edge_type": string(edge.EdgeType),
		"weight":    edge.Weight,
	})
	return err
}

func (s *Storage) GetNode(ctx context.Context, id string) (*CodeNode, error) {
	node, err := surrealdb.Select[CodeNode](ctx, s.db, "nodes:"+id)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetTransitiveDependencies returns all nodes that nodeID depends on, up to the specified depth.
// Uses iterative BFS to traverse the dependency graph.
func (s *Storage) GetTransitiveDependencies(ctx context.Context, nodeID string, depth int) ([]CodeNode, error) {
	if depth <= 0 {
		depth = 3
	}

	visited := make(map[string]bool)
	var result []CodeNode

	// BFS queue: start with direct dependencies of nodeID
	currentLevel := []string{nodeID}
	visited[nodeID] = true

	for level := 0; level < depth && len(currentLevel) > 0; level++ {
		// Get all edges from current level nodes
		var nextLevel []string

		for _, currentID := range currentLevel {
			// Query edges where this node is the source (outgoing dependencies)
			query := `SELECT * FROM edges WHERE from_id = $id`
			edgeResults, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
				"id": currentID,
			})
			if err != nil {
				continue
			}

			if edgeResults == nil || len(*edgeResults) == 0 {
				continue
			}

			edges := (*edgeResults)[0].Result
			for _, edge := range edges {
				if !visited[edge.ToID] {
					visited[edge.ToID] = true
					nextLevel = append(nextLevel, edge.ToID)
				}
			}
		}

		// Fetch node details for the next level
		for _, nid := range nextLevel {
			node, err := s.findNodeByID(ctx, nid)
			if err == nil && node != nil {
				result = append(result, *node)
			}
		}

		currentLevel = nextLevel
	}

	return result, nil
}

// TraceCallChain finds a path of function calls from 'from' to 'to'.
// Uses BFS to find the shortest path through call edges.
func (s *Storage) TraceCallChain(ctx context.Context, from, to string) ([]CodeEdge, error) {
	// First, try to find nodes by name if not full IDs
	fromID, err := s.resolveNodeID(ctx, from)
	if err != nil {
		fromID = from // Use as-is if resolution fails
	}

	toID, err := s.resolveNodeID(ctx, to)
	if err != nil {
		toID = to
	}

	// BFS to find path
	type pathState struct {
		nodeID string
		path   []CodeEdge
	}

	visited := make(map[string]bool)
	queue := []pathState{{nodeID: fromID, path: []CodeEdge{}}}
	visited[fromID] = true

	maxDepth := 15 // Prevent infinite loops

	for len(queue) > 0 && len(queue[0].path) < maxDepth {
		current := queue[0]
		queue = queue[1:]

		// Get outgoing call edges from current node
		query := `SELECT * FROM edges WHERE from_id = $id AND edge_type = $edgeType`
		edgeResults, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
			"id":       current.nodeID,
			"edgeType": string(EdgeTypeCalls),
		})
		if err != nil {
			continue
		}

		if edgeResults == nil || len(*edgeResults) == 0 {
			continue
		}

		edges := (*edgeResults)[0].Result
		for _, edge := range edges {
			newPath := make([]CodeEdge, len(current.path)+1)
			copy(newPath, current.path)
			newPath[len(current.path)] = edge

			// Check if we reached the target
			if edge.ToID == toID || s.nodeNameMatches(ctx, edge.ToID, to) {
				return newPath, nil
			}

			if !visited[edge.ToID] {
				visited[edge.ToID] = true
				queue = append(queue, pathState{
					nodeID: edge.ToID,
					path:   newPath,
				})
			}
		}
	}

	// No path found
	return []CodeEdge{}, nil
}

// SemanticSearch finds nodes similar to the query embedding using cosine similarity.
// Fetches all nodes with embeddings and ranks them by similarity.
func (s *Storage) SemanticSearch(ctx context.Context, queryEmbedding []float32, limit int) ([]CodeNode, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is empty")
	}

	// Fetch all nodes that have embeddings
	// Note: For large codebases, this should be paginated or use an external vector DB
	query := `SELECT * FROM nodes WHERE embedding != NONE`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch nodes: %w", err)
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	nodes := (*results)[0].Result

	// Calculate cosine similarity for each node
	var scored []ScoredNode
	for _, node := range nodes {
		if len(node.Embedding) == 0 {
			continue
		}

		// Ensure embedding dimensions match
		if len(node.Embedding) != len(queryEmbedding) {
			continue
		}

		similarity := cosineSimilarity(queryEmbedding, node.Embedding)
		scored = append(scored, ScoredNode{
			Node:  node,
			Score: similarity,
		})
	}

	// Sort by similarity (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Return top 'limit' results
	var result []CodeNode
	for i := 0; i < len(scored) && i < limit; i++ {
		// Only include results with positive similarity
		if scored[i].Score > 0 {
			result = append(result, scored[i].Node)
		}
	}

	return result, nil
}

// cosineSimilarity calculates the cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// resolveNodeID tries to find a node ID from a name
func (s *Storage) resolveNodeID(ctx context.Context, nameOrID string) (string, error) {
	// First check if it's already a valid ID
	query := `SELECT id FROM nodes WHERE id = $id`
	results, err := surrealdb.Query[[]struct{ ID string }](ctx, s.db, query, map[string]any{
		"id": nameOrID,
	})
	if err == nil && results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
		return (*results)[0].Result[0].ID, nil
	}

	// Try to find by name
	query = `SELECT id FROM nodes WHERE name = $name LIMIT 1`
	results, err = surrealdb.Query[[]struct{ ID string }](ctx, s.db, query, map[string]any{
		"name": nameOrID,
	})
	if err == nil && results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
		return (*results)[0].Result[0].ID, nil
	}

	// Try partial name match
	query = `SELECT id FROM nodes WHERE name CONTAINS $name LIMIT 1`
	results, err = surrealdb.Query[[]struct{ ID string }](ctx, s.db, query, map[string]any{
		"name": nameOrID,
	})
	if err == nil && results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
		return (*results)[0].Result[0].ID, nil
	}

	return "", fmt.Errorf("node not found: %s", nameOrID)
}

// findNodeByID retrieves a node by its ID
func (s *Storage) findNodeByID(ctx context.Context, id string) (*CodeNode, error) {
	query := `SELECT * FROM nodes WHERE id = $id LIMIT 1`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, map[string]any{
		"id": id,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, fmt.Errorf("node not found: %s", id)
	}

	return &(*results)[0].Result[0], nil
}

// nodeNameMatches checks if a node ID corresponds to a given name
func (s *Storage) nodeNameMatches(ctx context.Context, nodeID, name string) bool {
	node, err := s.findNodeByID(ctx, nodeID)
	if err != nil {
		return false
	}
	return node.Name == name
}

// GetAllEdges retrieves all edges from the graph
func (s *Storage) GetAllEdges(ctx context.Context) ([]CodeEdge, error) {
	query := `SELECT * FROM edges`
	results, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, nil)
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// GetEdgesByType retrieves edges filtered by type
func (s *Storage) GetEdgesByType(ctx context.Context, edgeType EdgeType) ([]CodeEdge, error) {
	query := `SELECT * FROM edges WHERE edge_type = $edgeType`
	results, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"edgeType": string(edgeType),
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

func (s *Storage) FindByName(ctx context.Context, name string) ([]CodeNode, error) {
	query := `SELECT * FROM nodes WHERE name CONTAINS $name`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, map[string]any{
		"name": name,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

func (s *Storage) GetNodesByFile(ctx context.Context, filePath string) ([]CodeNode, error) {
	query := `SELECT * FROM nodes WHERE file_path = $path`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

func (s *Storage) GetAllNodes(ctx context.Context) ([]CodeNode, error) {
	query := `SELECT * FROM nodes`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, nil)
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

func (s *Storage) DeleteNodesByFile(ctx context.Context, filePath string) error {
	query := `DELETE FROM nodes WHERE file_path = $path`
	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	return err
}

// GetIncomingEdges returns all edges pointing to a node
func (s *Storage) GetIncomingEdges(ctx context.Context, nodeID string) ([]CodeEdge, error) {
	query := `SELECT * FROM edges WHERE to_id = $id`
	results, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"id": nodeID,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// GetOutgoingEdges returns all edges from a node
func (s *Storage) GetOutgoingEdges(ctx context.Context, nodeID string) ([]CodeEdge, error) {
	query := `SELECT * FROM edges WHERE from_id = $id`
	results, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"id": nodeID,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// GetCallers returns all nodes that call the given node
func (s *Storage) GetCallers(ctx context.Context, nodeID string) ([]CodeNode, error) {
	// First get incoming call edges
	query := `SELECT * FROM edges WHERE to_id = $id AND edge_type = $edgeType`
	edgeResults, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"id":       nodeID,
		"edgeType": string(EdgeTypeCalls),
	})
	if err != nil {
		return nil, err
	}

	if edgeResults == nil || len(*edgeResults) == 0 {
		return nil, nil
	}

	edges := (*edgeResults)[0].Result
	var nodes []CodeNode
	for _, edge := range edges {
		node, err := s.findNodeByID(ctx, edge.FromID)
		if err == nil && node != nil {
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

// GetCallees returns all nodes that are called by the given node
func (s *Storage) GetCallees(ctx context.Context, nodeID string) ([]CodeNode, error) {
	// Get outgoing call edges
	query := `SELECT * FROM edges WHERE from_id = $id AND edge_type = $edgeType`
	edgeResults, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"id":       nodeID,
		"edgeType": string(EdgeTypeCalls),
	})
	if err != nil {
		return nil, err
	}

	if edgeResults == nil || len(*edgeResults) == 0 {
		return nil, nil
	}

	edges := (*edgeResults)[0].Result
	var nodes []CodeNode
	for _, edge := range edges {
		node, err := s.findNodeByID(ctx, edge.ToID)
		if err == nil && node != nil {
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

func (s *Storage) RunMigrations(ctx context.Context) error {
	migrations := []string{
		`DEFINE TABLE nodes SCHEMAFULL`,
		`DEFINE FIELD id ON nodes TYPE string`,
		`DEFINE FIELD name ON nodes TYPE string`,
		`DEFINE FIELD node_type ON nodes TYPE option<string>`,
		`DEFINE FIELD language ON nodes TYPE string`,
		`DEFINE FIELD file_path ON nodes TYPE string`,
		`DEFINE FIELD start_line ON nodes TYPE int`,
		`DEFINE FIELD end_line ON nodes TYPE int`,
		`DEFINE FIELD content ON nodes TYPE option<string>`,
		`DEFINE FIELD doc_comment ON nodes TYPE option<string>`,
		`DEFINE FIELD annotations ON nodes TYPE option<object>`,
		`DEFINE FIELD embedding ON nodes TYPE option<array<float>>`,
		`DEFINE FIELD complexity ON nodes TYPE option<float>`,
		`DEFINE INDEX idx_nodes_id ON nodes FIELDS id UNIQUE`,
		`DEFINE INDEX idx_nodes_file ON nodes FIELDS file_path`,
		`DEFINE INDEX idx_nodes_name ON nodes FIELDS name`,
		`DEFINE INDEX idx_nodes_type ON nodes FIELDS node_type`,

		`DEFINE TABLE edges SCHEMAFULL`,
		`DEFINE FIELD id ON edges TYPE string`,
		`DEFINE FIELD from_id ON edges TYPE string`,
		`DEFINE FIELD to_id ON edges TYPE string`,
		`DEFINE FIELD edge_type ON edges TYPE string`,
		`DEFINE FIELD weight ON edges TYPE float DEFAULT 1.0`,
		`DEFINE INDEX idx_edges_id ON edges FIELDS id UNIQUE`,
		`DEFINE INDEX idx_edges_from ON edges FIELDS from_id`,
		`DEFINE INDEX idx_edges_to ON edges FIELDS to_id`,
		`DEFINE INDEX idx_edges_type ON edges FIELDS edge_type`,
		`DEFINE INDEX idx_edges_from_type ON edges FIELDS from_id, edge_type`,
		`DEFINE INDEX idx_edges_to_type ON edges FIELDS to_id, edge_type`,
	}

	for _, m := range migrations {
		if _, err := surrealdb.Query[any](ctx, s.db, m, nil); err != nil {
			// Ignore "already exists" errors
			continue
		}
	}

	return nil
}
