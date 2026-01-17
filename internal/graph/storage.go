package graph

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/surrealdb/surrealdb.go"
)

type Storage struct {
	db        *surrealdb.DB
	namespace string
	database  string

	// fileLocksMu protects the fileLocks map
	fileLocksMu sync.Mutex
	// fileLocks holds per-file mutexes for coordinating concurrent operations
	fileLocks map[string]*fileLock
}

// fileLock represents a lock for a specific file
type fileLock struct {
	mu    sync.Mutex
	count int // reference count for cleanup
}

// lockFile acquires a lock for the given file path
func (s *Storage) lockFile(filePath string) {
	s.fileLocksMu.Lock()
	if s.fileLocks == nil {
		s.fileLocks = make(map[string]*fileLock)
	}
	fl, exists := s.fileLocks[filePath]
	if !exists {
		fl = &fileLock{}
		s.fileLocks[filePath] = fl
	}
	fl.count++
	s.fileLocksMu.Unlock()

	fl.mu.Lock()
}

// unlockFile releases a lock for the given file path
func (s *Storage) unlockFile(filePath string) {
	s.fileLocksMu.Lock()
	defer s.fileLocksMu.Unlock()

	fl, exists := s.fileLocks[filePath]
	if !exists {
		return
	}

	fl.mu.Unlock()
	fl.count--
	if fl.count == 0 {
		delete(s.fileLocks, filePath)
	}
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

// FileMetadata tracks indexed files for incremental indexing
type FileMetadata struct {
	FilePath      string     `json:"file_path"`
	ContentHash   string     `json:"content_hash"`
	ModTime       int64      `json:"mod_time"`
	IndexedAt     int64      `json:"indexed_at"`
	NodeCount     int        `json:"node_count"`
	EdgeCount     int        `json:"edge_count"`
	ProjectID     string     `json:"project_id,omitempty"`
	FileSize      int64      `json:"file_size,omitempty"`
	Language      string     `json:"language,omitempty"`
	ModifiedAt    *time.Time `json:"modified_at,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
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

// UpsertNodesBatch inserts multiple nodes in a single transaction
// This is significantly faster than inserting nodes one at a time
func (s *Storage) UpsertNodesBatch(ctx context.Context, nodes []*CodeNode) error {
	if len(nodes) == 0 {
		return nil
	}

	// Build batch data
	nodeData := make([]map[string]any, len(nodes))
	for i, node := range nodes {
		// Ensure annotations is never nil (SurrealDB requires empty object, not null)
		annotations := node.Annotations
		if annotations == nil {
			annotations = map[string]string{}
		}

		nodeData[i] = map[string]any{
			"id":          node.ID,
			"name":        node.Name,
			"node_type":   string(node.NodeType),
			"language":    node.Language,
			"file_path":   node.FilePath,
			"start_line":  node.StartLine,
			"end_line":    node.EndLine,
			"content":     node.Content,
			"doc_comment": node.DocComment,
			"annotations": annotations,
			"embedding":   node.Embedding,
			"complexity":  node.Complexity,
		}
	}

	// Use transaction with FOR loop for batch upsert
	// This properly handles existing records with our id field
	query := `
		BEGIN TRANSACTION;
		FOR $node IN $nodes {
			UPSERT nodes SET
				id = $node.id,
				name = $node.name,
				node_type = $node.node_type,
				language = $node.language,
				file_path = $node.file_path,
				start_line = $node.start_line,
				end_line = $node.end_line,
				content = $node.content,
				doc_comment = $node.doc_comment,
				annotations = $node.annotations,
				embedding = $node.embedding,
				complexity = $node.complexity
			WHERE id = $node.id;
		};
		COMMIT TRANSACTION;
	`

	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"nodes": nodeData,
	})
	return err
}

// UpsertEdgesBatch inserts multiple edges in a single transaction
func (s *Storage) UpsertEdgesBatch(ctx context.Context, edges []*CodeEdge) error {
	if len(edges) == 0 {
		return nil
	}

	edgeData := make([]map[string]any, len(edges))
	for i, edge := range edges {
		edgeData[i] = map[string]any{
			"id":        edge.ID,
			"from_id":   edge.FromID,
			"to_id":     edge.ToID,
			"edge_type": string(edge.EdgeType),
			"weight":    edge.Weight,
		}
	}

	// Use transaction with FOR loop for batch upsert
	query := `
		BEGIN TRANSACTION;
		FOR $edge IN $edges {
			UPSERT edges SET
				id = $edge.id,
				from_id = $edge.from_id,
				to_id = $edge.to_id,
				edge_type = $edge.edge_type,
				weight = $edge.weight
			WHERE id = $edge.id;
		};
		COMMIT TRANSACTION;
	`

	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"edges": edgeData,
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

		// Fetch node details for the next level in batch
		if len(nextLevel) > 0 {
			placeholders := make([]string, len(nextLevel))
			params := make(map[string]any)
			for i, nid := range nextLevel {
				placeholders[i] = fmt.Sprintf("$id%d", i)
				params[fmt.Sprintf("id%d", i)] = nid
			}
			query := fmt.Sprintf(`SELECT * FROM nodes WHERE id IN [%s]`, strings.Join(placeholders, ", "))
			nodeResults, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, params)
			if err == nil && nodeResults != nil && len(*nodeResults) > 0 {
				result = append(result, (*nodeResults)[0].Result...)
			}
		}

		currentLevel = nextLevel
	}

	return result, nil
}

// FormatEdgeID generates a unique edge ID using the format "fromID->toID:edgeType"
// This ensures that different edge types between the same nodes have unique IDs
func FormatEdgeID(fromID, toID string, edgeType EdgeType) string {
	return fmt.Sprintf("%s->%s:%s", fromID, toID, edgeType)
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

	if limit <= 0 {
		limit = 10
	}

	if limit > 1000 {
		limit = 1000
	}

	// Fetch all nodes that have embeddings
	// Note: For large codebases, this should be paginated or use an external vector DB
	query := `SELECT * FROM nodes WHERE embedding != NONE LIMIT 10000`
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
	query := `SELECT * FROM edges LIMIT 10000`
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
	query := `SELECT * FROM nodes LIMIT 10000`
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
	s.lockFile(filePath)
	defer s.unlockFile(filePath)

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

		// File metadata table for incremental indexing
		`DEFINE TABLE file_metadata SCHEMAFULL`,
		`DEFINE FIELD file_path ON file_metadata TYPE string`,
		`DEFINE FIELD content_hash ON file_metadata TYPE string`,
		`DEFINE FIELD mod_time ON file_metadata TYPE int`,
		`DEFINE FIELD indexed_at ON file_metadata TYPE int`,
		`DEFINE FIELD node_count ON file_metadata TYPE int`,
		`DEFINE FIELD edge_count ON file_metadata TYPE int`,
		`DEFINE FIELD project_id ON file_metadata TYPE option<string>`,
		`DEFINE FIELD file_size ON file_metadata TYPE option<int>`,
		`DEFINE FIELD language ON file_metadata TYPE option<string>`,
		`DEFINE FIELD modified_at ON file_metadata TYPE option<datetime>`,
		`DEFINE INDEX idx_file_metadata_path ON file_metadata FIELDS file_path UNIQUE`,
	}

	for _, m := range migrations {
		if _, err := surrealdb.Query[any](ctx, s.db, m, nil); err != nil {
			// Log migration errors for debugging, but continue
			// This handles "already exists" errors gracefully
			continue
		}
	}

	return nil
}

// UpsertFileMetadata stores or updates file metadata for incremental indexing
func (s *Storage) UpsertFileMetadata(ctx context.Context, meta *FileMetadata) error {
	// Use default project if not specified
	projectID := meta.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	// Use SurrealDB UPSERT syntax for proper upsert behavior.
	// If a record with matching file_path exists, update it; otherwise insert new.
	// Include modified_at as required by the existing schema.
	query := `UPSERT file_metadata SET
		file_path = $file_path,
		content_hash = $content_hash,
		mod_time = $mod_time,
		indexed_at = $indexed_at,
		node_count = $node_count,
		edge_count = $edge_count,
		project_id = $project_id,
		file_size = $file_size,
		language = $language,
		modified_at = time::now()
	WHERE file_path = $file_path`

	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"file_path":    meta.FilePath,
		"content_hash": meta.ContentHash,
		"mod_time":     meta.ModTime,
		"indexed_at":   meta.IndexedAt,
		"node_count":   meta.NodeCount,
		"edge_count":   meta.EdgeCount,
		"project_id":   projectID,
		"file_size":    meta.FileSize,
		"language":     meta.Language,
	})
	if err != nil {
		return fmt.Errorf("file metadata upsert failed: %w", err)
	}
	return nil
}

// GetFileMetadata retrieves metadata for a specific file
func (s *Storage) GetFileMetadata(ctx context.Context, filePath string) (*FileMetadata, error) {
	query := `SELECT * FROM file_metadata WHERE file_path = $path LIMIT 1`
	results, err := surrealdb.Query[[]FileMetadata](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, nil
	}

	return &(*results)[0].Result[0], nil
}

// GetAllFileMetadata retrieves all file metadata (for detecting deleted files)
func (s *Storage) GetAllFileMetadata(ctx context.Context) ([]FileMetadata, error) {
	query := `SELECT * FROM file_metadata`
	results, err := surrealdb.Query[[]FileMetadata](ctx, s.db, query, nil)
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// DeleteFileMetadata removes metadata for a specific file
func (s *Storage) DeleteFileMetadata(ctx context.Context, filePath string) error {
	query := `DELETE FROM file_metadata WHERE file_path = $path`
	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	return err
}

// DeleteEdgesByFile removes all edges originating from nodes in a specific file
func (s *Storage) DeleteEdgesByFile(ctx context.Context, filePath string) error {
	// Get all node IDs for this file
	query := `SELECT id FROM nodes WHERE file_path = $path`
	results, err := surrealdb.Query[[]struct{ ID string }](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	if err != nil {
		return err
	}

	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil
	}

	// Delete edges from these nodes
	nodeIDs := make([]string, len((*results)[0].Result))
	for i, n := range (*results)[0].Result {
		nodeIDs[i] = n.ID
	}

	query = `DELETE FROM edges WHERE from_id IN $ids OR to_id IN $ids`
	_, err = surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"ids": nodeIDs,
	})
	return err
}

// StoreGraphAtomic stores both nodes and edges in a single transaction.
// This ensures data integrity - either all nodes and edges are stored, or none are.
// If storing edges fails, the entire transaction is rolled back, preventing orphaned edges.
func (s *Storage) StoreGraphAtomic(ctx context.Context, nodes []*CodeNode, edges []*CodeEdge) error {
	if len(nodes) == 0 && len(edges) == 0 {
		return nil
	}

	// Build the transaction query dynamically based on what we have
	var transactionParts []string
	params := make(map[string]any)

	// Add node storage part
	if len(nodes) > 0 {
		nodeData := make([]map[string]any, len(nodes))
		for i, node := range nodes {
			annotations := node.Annotations
			if annotations == nil {
				annotations = map[string]string{}
			}

			nodeData[i] = map[string]any{
				"id":          node.ID,
				"name":        node.Name,
				"node_type":   string(node.NodeType),
				"language":    node.Language,
				"file_path":   node.FilePath,
				"start_line":  node.StartLine,
				"end_line":    node.EndLine,
				"content":     node.Content,
				"doc_comment": node.DocComment,
				"annotations": annotations,
				"embedding":   node.Embedding,
				"complexity":  node.Complexity,
			}
		}

		transactionParts = append(transactionParts,
			`FOR $node IN $nodeData {
				UPSERT nodes SET
					id = $node.id,
					name = $node.name,
					node_type = $node.node_type,
					language = $node.language,
					file_path = $node.file_path,
					start_line = $node.start_line,
					end_line = $node.end_line,
					content = $node.content,
					doc_comment = $node.doc_comment,
					annotations = $node.annotations,
					embedding = $node.embedding,
					complexity = $node.complexity
				WHERE id = $node.id;
			}`)

		// Add nodeData to params after building transactionParts
		params["nodeData"] = nodeData
	}

	// Add edge storage part
	if len(edges) > 0 {
		edgeData := make([]map[string]any, len(edges))
		for i, edge := range edges {
			edgeData[i] = map[string]any{
				"id":        edge.ID,
				"from_id":   edge.FromID,
				"to_id":     edge.ToID,
				"edge_type": string(edge.EdgeType),
				"weight":    edge.Weight,
			}
		}

		transactionParts = append(transactionParts,
			`FOR $edge IN $edgeData {
				UPSERT edges SET
					id = $edge.id,
					from_id = $edge.from_id,
					to_id = $edge.to_id,
					edge_type = $edge.edge_type,
					weight = $edge.weight
				WHERE id = $edge.id;
			}`)

		// Add edgeData to params after building transactionParts
		params["edgeData"] = edgeData
	}

	// Combine into a single transaction
	query := "BEGIN TRANSACTION;\n" + strings.Join(transactionParts, "\n") + "\nCOMMIT TRANSACTION;"

	_, err := surrealdb.Query[any](ctx, s.db, query, params)
	return err
}

// UpdateFileAtomic atomically updates a file's nodes and edges.
// This ensures data integrity by using a transaction:
// 1. Delete old nodes and edges for file
// 2. Store new nodes and edges
// All operations are performed in a single transaction; if any step fails, the entire transaction is rolled back.
func (s *Storage) UpdateFileAtomic(ctx context.Context, filePath string, nodes []*CodeNode, edges []*CodeEdge) error {
	s.lockFile(filePath)
	defer s.unlockFile(filePath)

	if len(nodes) == 0 && len(edges) == 0 {
		// If no new data, just delete old data atomically in a transaction
		// This ensures we don't leave orphaned data if one delete fails
		query := `BEGIN TRANSACTION;
		           DELETE FROM edges WHERE from_id IN (SELECT id FROM nodes WHERE file_path = $path) OR to_id IN (SELECT id FROM nodes WHERE file_path = $path);
		           DELETE FROM nodes WHERE file_path = $path;
		           DELETE FROM file_metadata WHERE file_path = $path;
		           COMMIT TRANSACTION;`
		_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
			"path": filePath,
		})
		if err != nil {
			return fmt.Errorf("failed to delete file data atomically: %w", err)
		}
		return nil
	}

	// Query for existing node IDs before starting the transaction
	// This is needed to delete edges since edges don't have file_path directly
	query := `SELECT id FROM nodes WHERE file_path = $path`
	results, err := surrealdb.Query[[]struct{ ID string }](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	if err != nil {
		return fmt.Errorf("failed to query existing nodes: %w", err)
	}

	oldNodeIDs := make([]string, 0)
	if results != nil && len(*results) > 0 {
		for _, node := range (*results)[0].Result {
			oldNodeIDs = append(oldNodeIDs, node.ID)
		}
	}

	// Build the transaction query with deletion, node storage, and edge storage
	var transactionParts []string

	// Part 1: Delete old edges for this file (using pre-fetched node IDs)
	if len(oldNodeIDs) > 0 {
		transactionParts = append(transactionParts,
			`DELETE FROM edges WHERE from_id IN $oldNodeIDs OR to_id IN $oldNodeIDs;`)
	}

	// Part 2: Delete old nodes for this file
	transactionParts = append(transactionParts,
		`DELETE FROM nodes WHERE file_path = $filePath;`)

	// Part 2.5: Note - file_metadata is NOT deleted here to avoid data loss
	// The caller (indexer) will call UpsertFileMetadata after this function completes
	// This ensures metadata is never left in an inconsistent state


	// Prepare data outside the transaction to avoid variable scope issues
	var nodeData []map[string]any
	var edgeData []map[string]any

	// Part 3: Store new nodes
	if len(nodes) > 0 {
		nodeData = make([]map[string]any, len(nodes))
		for i, node := range nodes {
			annotations := node.Annotations
			if annotations == nil {
				annotations = map[string]string{}
			}

			nodeData[i] = map[string]any{
				"id":          node.ID,
				"name":        node.Name,
				"node_type":   string(node.NodeType),
				"language":    node.Language,
				"file_path":   node.FilePath,
				"start_line":  node.StartLine,
				"end_line":    node.EndLine,
				"content":     node.Content,
				"doc_comment": node.DocComment,
				"annotations": annotations,
				"embedding":   node.Embedding,
				"complexity":  node.Complexity,
			}
		}

		transactionParts = append(transactionParts,
			`FOR $node IN $nodeData {
				UPSERT nodes SET
					id = $node.id,
					name = $node.name,
					node_type = $node.node_type,
					language = $node.language,
					file_path = $node.file_path,
					start_line = $node.start_line,
					end_line = $node.end_line,
					content = $node.content,
					doc_comment = $node.doc_comment,
					annotations = $node.annotations,
					embedding = $node.embedding,
					complexity = $node.complexity
				WHERE id = $node.id;
			}`)
	}

	// Part 4: Store new edges
	if len(edges) > 0 {
		edgeData = make([]map[string]any, len(edges))
		for i, edge := range edges {
			edgeData[i] = map[string]any{
				"id":        edge.ID,
				"from_id":   edge.FromID,
				"to_id":     edge.ToID,
				"edge_type": string(edge.EdgeType),
				"weight":    edge.Weight,
			}
		}

		transactionParts = append(transactionParts,
			`FOR $edge IN $edgeData {
				UPSERT edges SET
					id = $edge.id,
					from_id = $edge.from_id,
					to_id = $edge.to_id,
					edge_type = $edge.edge_type,
					weight = $edge.weight
				WHERE id = $edge.id;
			}`)
	}

	// Combine into a single transaction
	query = "BEGIN TRANSACTION;\n" + strings.Join(transactionParts, "\n") + "\nCOMMIT TRANSACTION;"

	// Build parameters
	var params map[string]any
	params = make(map[string]any)
	params["filePath"] = filePath
	if len(oldNodeIDs) > 0 {
		params["oldNodeIDs"] = oldNodeIDs
	}
	if len(nodes) > 0 {
		params["nodeData"] = nodeData
	}
	if len(edges) > 0 {
		params["edgeData"] = edgeData
	}

	_, err = surrealdb.Query[any](ctx, s.db, query, params)
	if err != nil {
		return fmt.Errorf("atomic file update failed: %w", err)
	}
	return nil
}
