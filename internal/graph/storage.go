package graph

import (
	"context"
	"fmt"

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
	_, err := surrealdb.Create[CodeNode](ctx, s.db, "nodes", node)
	return err
}

func (s *Storage) UpsertEdge(ctx context.Context, edge *CodeEdge) error {
	_, err := surrealdb.Create[CodeEdge](ctx, s.db, "edges", edge)
	return err
}

func (s *Storage) GetNode(ctx context.Context, id string) (*CodeNode, error) {
	node, err := surrealdb.Select[CodeNode](ctx, s.db, "nodes:"+id)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (s *Storage) GetTransitiveDependencies(ctx context.Context, nodeID string, depth int) ([]CodeNode, error) {
	query := `
		SELECT * FROM nodes
		WHERE id IN (
			SELECT ->edges->nodes.id FROM nodes:$id
		)
	`
	// Note: This is simplified - real implementation needs recursive CTE
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, map[string]any{
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

func (s *Storage) TraceCallChain(ctx context.Context, from, to string) ([]CodeEdge, error) {
	query := `
		SELECT * FROM edges
		WHERE from_id = $from AND edge_type = 'calls'
	`
	results, err := surrealdb.Query[[]CodeEdge](ctx, s.db, query, map[string]any{
		"from": from,
		"to":   to,
	})
	if err != nil {
		return nil, err
	}

	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

func (s *Storage) SemanticSearch(ctx context.Context, embedding []float32, limit int) ([]CodeNode, error) {
	// SurrealDB doesn't have native vector search yet
	// This would need a custom implementation or external service
	query := `SELECT * FROM nodes LIMIT $limit`
	results, err := surrealdb.Query[[]CodeNode](ctx, s.db, query, map[string]any{
		"limit": limit,
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

func (s *Storage) DeleteNodesByFile(ctx context.Context, filePath string) error {
	query := `DELETE FROM nodes WHERE file_path = $path`
	_, err := surrealdb.Query[any](ctx, s.db, query, map[string]any{
		"path": filePath,
	})
	return err
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
		`DEFINE FIELD embedding ON nodes TYPE option<array<float>>`,
		`DEFINE FIELD complexity ON nodes TYPE option<float>`,
		`DEFINE INDEX idx_nodes_id ON nodes FIELDS id UNIQUE`,
		`DEFINE INDEX idx_nodes_file ON nodes FIELDS file_path`,
		`DEFINE INDEX idx_nodes_name ON nodes FIELDS name`,

		`DEFINE TABLE edges SCHEMAFULL`,
		`DEFINE FIELD id ON edges TYPE string`,
		`DEFINE FIELD from_id ON edges TYPE string`,
		`DEFINE FIELD to_id ON edges TYPE string`,
		`DEFINE FIELD edge_type ON edges TYPE string`,
		`DEFINE FIELD weight ON edges TYPE float DEFAULT 1.0`,
		`DEFINE INDEX idx_edges_from ON edges FIELDS from_id`,
		`DEFINE INDEX idx_edges_to ON edges FIELDS to_id`,
	}

	for _, m := range migrations {
		if _, err := surrealdb.Query[any](ctx, s.db, m, nil); err != nil {
			// Ignore "already exists" errors
			continue
		}
	}

	return nil
}
