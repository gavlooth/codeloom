package indexer

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
)

// Status represents the current indexing status
type Status struct {
	State         string    `json:"state"` // idle, indexing, watching, error
	Directory     string    `json:"directory,omitempty"`
	FilesTotal    int64     `json:"files_total"`
	FilesIndexed  int64     `json:"files_indexed"`
	NodesCreated  int64     `json:"nodes_created"`
	EdgesCreated  int64     `json:"edges_created"`
	Errors        []string  `json:"errors,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

// Indexer handles codebase indexing operations
type Indexer struct {
	parser    *parser.Parser
	storage   *graph.Storage
	embedding embedding.Provider

	mu              sync.RWMutex
	status          Status
	excludePatterns []string
}

// Config holds indexer configuration
type Config struct {
	Parser          *parser.Parser
	Storage         *graph.Storage
	Embedding       embedding.Provider // optional
	ExcludePatterns []string
}

// New creates a new Indexer
func New(cfg Config) *Indexer {
	if cfg.ExcludePatterns == nil {
		cfg.ExcludePatterns = DefaultExcludePatterns()
	}
	return &Indexer{
		parser:          cfg.Parser,
		storage:         cfg.Storage,
		embedding:       cfg.Embedding,
		excludePatterns: cfg.ExcludePatterns,
		status: Status{
			State: "idle",
		},
	}
}

// DefaultExcludePatterns returns common patterns to exclude from indexing
func DefaultExcludePatterns() []string {
	return []string{
		".git",
		".svn",
		".hg",
		"node_modules",
		"vendor",
		"__pycache__",
		".venv",
		"venv",
		"target",
		"build",
		"dist",
		".idea",
		".vscode",
		"*.min.js",
		"*.min.css",
		"*.map",
		".codeloom",
	}
}

// GetStatus returns the current indexing status
func (idx *Indexer) GetStatus() Status {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.status
}

// IndexDirectory indexes all supported files in a directory
func (idx *Indexer) IndexDirectory(ctx context.Context, dir string, progressCb func(Status)) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory: %w", err)
	}

	// Initialize status
	idx.mu.Lock()
	idx.status = Status{
		State:      "indexing",
		Directory:  absDir,
		StartedAt:  time.Now(),
		Errors:     []string{},
	}
	idx.mu.Unlock()

	// Ensure migrations are run
	if err := idx.storage.RunMigrations(ctx); err != nil {
		log.Printf("Warning: migration error (may be okay): %v", err)
	}

	// Parse the directory
	result, err := idx.parser.ParseDirectory(ctx, absDir, idx.excludePatterns)
	if err != nil {
		idx.setError(fmt.Sprintf("parse error: %v", err))
		return err
	}

	idx.mu.Lock()
	idx.status.FilesTotal = int64(len(result.Nodes))
	idx.mu.Unlock()

	if progressCb != nil {
		progressCb(idx.GetStatus())
	}

	// Store nodes
	var nodesCreated, edgesCreated int64
	var indexErrors []string

	for _, node := range result.Nodes {
		select {
		case <-ctx.Done():
			idx.setError("indexing cancelled")
			return ctx.Err()
		default:
		}

		// Generate embedding if provider is available
		var emb []float32
		if idx.embedding != nil && node.Content != "" {
			emb, err = idx.embedding.EmbedSingle(ctx, node.Content)
			if err != nil {
				log.Printf("Warning: embedding failed for %s: %v", node.ID, err)
			}
		}

		// Convert to graph node
		graphNode := &graph.CodeNode{
			ID:          node.ID,
			Name:        node.Name,
			NodeType:    graph.NodeType(node.NodeType),
			Language:    string(node.Language),
			FilePath:    node.FilePath,
			StartLine:   node.StartLine,
			EndLine:     node.EndLine,
			Content:     node.Content,
			DocComment:  node.DocComment,
			Annotations: node.Annotations,
			Embedding:   emb,
		}

		if err := idx.storage.UpsertNode(ctx, graphNode); err != nil {
			errMsg := fmt.Sprintf("failed to store node %s: %v", node.ID, err)
			indexErrors = append(indexErrors, errMsg)
			log.Printf("Warning: %s", errMsg)
		} else {
			atomic.AddInt64(&nodesCreated, 1)
		}

		atomic.AddInt64(&idx.status.FilesIndexed, 1)

		if progressCb != nil && nodesCreated%10 == 0 {
			idx.mu.Lock()
			idx.status.NodesCreated = nodesCreated
			idx.mu.Unlock()
			progressCb(idx.GetStatus())
		}
	}

	// Store edges
	for _, edge := range result.Edges {
		select {
		case <-ctx.Done():
			idx.setError("indexing cancelled")
			return ctx.Err()
		default:
		}

		graphEdge := &graph.CodeEdge{
			ID:       fmt.Sprintf("%s->%s", edge.FromID, edge.ToID),
			FromID:   edge.FromID,
			ToID:     edge.ToID,
			EdgeType: graph.EdgeType(edge.EdgeType),
			Weight:   1.0,
		}

		if err := idx.storage.UpsertEdge(ctx, graphEdge); err != nil {
			errMsg := fmt.Sprintf("failed to store edge %s: %v", graphEdge.ID, err)
			indexErrors = append(indexErrors, errMsg)
		} else {
			atomic.AddInt64(&edgesCreated, 1)
		}
	}

	// Update final status
	idx.mu.Lock()
	idx.status.State = "idle"
	idx.status.NodesCreated = nodesCreated
	idx.status.EdgesCreated = edgesCreated
	idx.status.Errors = indexErrors
	idx.status.CompletedAt = time.Now()
	idx.mu.Unlock()

	if progressCb != nil {
		progressCb(idx.GetStatus())
	}

	return nil
}

// IndexFile indexes a single file
func (idx *Indexer) IndexFile(ctx context.Context, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Parse the file
	result, err := idx.parser.ParseFile(ctx, absPath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	// Delete existing nodes for this file
	if err := idx.storage.DeleteNodesByFile(ctx, absPath); err != nil {
		log.Printf("Warning: failed to delete existing nodes: %v", err)
	}

	// Store nodes
	for _, node := range result.Nodes {
		var emb []float32
		if idx.embedding != nil && node.Content != "" {
			var embErr error
			emb, embErr = idx.embedding.EmbedSingle(ctx, node.Content)
			if embErr != nil {
				log.Printf("Warning: embedding failed for %s: %v", node.ID, embErr)
				emb = nil // Continue without embedding
			}
		}

		graphNode := &graph.CodeNode{
			ID:          node.ID,
			Name:        node.Name,
			NodeType:    graph.NodeType(node.NodeType),
			Language:    string(node.Language),
			FilePath:    node.FilePath,
			StartLine:   node.StartLine,
			EndLine:     node.EndLine,
			Content:     node.Content,
			DocComment:  node.DocComment,
			Annotations: node.Annotations,
			Embedding:   emb,
		}

		if err := idx.storage.UpsertNode(ctx, graphNode); err != nil {
			return fmt.Errorf("failed to store node: %w", err)
		}
	}

	// Store edges
	for _, edge := range result.Edges {
		graphEdge := &graph.CodeEdge{
			ID:       fmt.Sprintf("%s->%s", edge.FromID, edge.ToID),
			FromID:   edge.FromID,
			ToID:     edge.ToID,
			EdgeType: graph.EdgeType(edge.EdgeType),
			Weight:   1.0,
		}

		if err := idx.storage.UpsertEdge(ctx, graphEdge); err != nil {
			return fmt.Errorf("failed to store edge: %w", err)
		}
	}

	return nil
}

// DeleteFile removes all nodes associated with a file from the index
func (idx *Indexer) DeleteFile(ctx context.Context, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	return idx.storage.DeleteNodesByFile(ctx, absPath)
}

func (idx *Indexer) setError(msg string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.status.State = "error"
	idx.status.LastError = msg
	idx.status.Errors = append(idx.status.Errors, msg)
}
