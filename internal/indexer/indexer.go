package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
	"github.com/heefoo/codeloom/internal/util"
)

// Status represents the current indexing status
type Status struct {
	State        string    `json:"state"` // idle, indexing, watching, error
	Directory    string    `json:"directory,omitempty"`
	FilesTotal   int64     `json:"files_total"`   // Total source files found
	FilesIndexed int64     `json:"files_indexed"` // Files successfully parsed
	FilesSkipped int64     `json:"files_skipped"` // Files skipped (unchanged)
	FilesDeleted int64     `json:"files_deleted"` // Files removed from index
	NodesTotal   int64     `json:"nodes_total"`   // Total code elements (functions, classes, etc.)
	NodesCreated int64     `json:"nodes_created"` // Code elements stored in DB
	EdgesCreated int64     `json:"edges_created"`
	Incremental  bool      `json:"incremental"` // Whether this was an incremental index
	Errors       []string  `json:"errors,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`

	// Embedding metrics to identify systemic issues
	EmbeddingSuccessCount int64 `json:"embedding_success_count"` // Total successful embeddings
	EmbeddingRetryCount   int64 `json:"embedding_retry_count"`   // Total retry attempts
	EmbeddingFailureCount int64 `json:"embedding_failure_count"` // Total nodes that failed after all retries
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

// retryEmbedding attempts to generate an embedding with exponential backoff retry
// It makes up to maxRetries attempts before giving up
func retryEmbedding(ctx context.Context, embProvider embedding.Provider, nodeID, content string, retryCount, successCount, failureCount *atomic.Int64) ([]float32, error) {
	const maxRetries = 3
	const initialBackoff = 500 * time.Millisecond // 500ms


	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Check context cancellation before each attempt
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Try to generate embedding
		emb, err := embProvider.EmbedSingle(ctx, content)
		if err == nil {
			successCount.Add(1)
			return emb, nil
		}
		lastErr = err

		// If this was the last attempt, don't wait
		if attempt == maxRetries-1 {
			break
		}

		// Increment retry counter
		retryCount.Add(1)

		// Calculate backoff with exponential growth
		backoff := time.Duration(1<<uint(attempt)) * initialBackoff
		log.Printf("Retrying embedding for %s (attempt %d/%d, backoff %v): %v", nodeID, attempt+1, maxRetries, backoff, err)

		// Wait for backoff duration or context cancellation
		select {
		case <-time.After(backoff):
			// Continue to next attempt
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// All retries failed
	failureCount.Add(1)
	return nil, fmt.Errorf("embedding failed after %d attempts: %w", maxRetries, lastErr)
}

// fileInfo holds information about a file for change detection
type fileInfo struct {
	Path    string
	ModTime int64
	Hash    string
}

// computeFileHash computes SHA256 hash of file content
func computeFileHash(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()

	// Use a buffered reader with context checking
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return "", err
		}
		if n == 0 {
			break
		}
		h.Write(buf[:n])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// IndexDirectory indexes all supported files in a directory
// Uses incremental indexing to only process changed files
func (idx *Indexer) IndexDirectory(ctx context.Context, dir string, progressCb func(Status)) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory: %w", err)
	}

	// Initialize status
	idx.mu.Lock()
	idx.status = Status{
		State:       "indexing",
		Directory:   absDir,
		StartedAt:   time.Now(),
		Errors:      []string{},
		Incremental: true, // Assume incremental until proven otherwise
	}
	idx.mu.Unlock()

	// Ensure migrations are run
	if err := idx.storage.RunMigrations(ctx); err != nil {
		log.Printf("Warning: migration error (may be okay): %v", err)
	}

	// Load existing file metadata
	existingMeta, err := idx.storage.GetAllFileMetadata(ctx)
	if err != nil {
		log.Printf("Warning: could not load file metadata: %v", err)
		existingMeta = nil
	}

	// Build map of existing files for quick lookup
	existingFiles := make(map[string]*graph.FileMetadata)
	for i := range existingMeta {
		existingFiles[existingMeta[i].FilePath] = &existingMeta[i]
	}

	// If no existing metadata, this is a full index
	if len(existingFiles) == 0 {
		idx.mu.Lock()
		idx.status.Incremental = false
		idx.mu.Unlock()
	}

	// Collect all source files and detect changes
	var changedFiles []string
	var unchangedFiles []string
	currentFiles := make(map[string]bool)

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}

		// Skip directories and apply exclude patterns
		if info.IsDir() {
			for _, pattern := range idx.excludePatterns {
				if util.MatchPattern(pattern, info.Name()) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check if file is supported
		if !idx.parser.IsSupportedFile(path) {
			return nil
		}

		currentFiles[path] = true

		// Check if file has changed
		existing, exists := existingFiles[path]
		if !exists {
			// New file
			changedFiles = append(changedFiles, path)
			return nil
		}

		// Check modification time first (fast check)
		if info.ModTime().Unix() != existing.ModTime {
			// Mod time changed, compute hash to verify
			hash, err := computeFileHash(ctx, path)
			if err != nil {
				log.Printf("Warning: could not hash file %s: %v", path, err)
				changedFiles = append(changedFiles, path)
				return nil
			}

			if hash != existing.ContentHash {
				changedFiles = append(changedFiles, path)
			} else {
				// Content same despite mod time change, skip
				unchangedFiles = append(unchangedFiles, path)
			}
		} else {
			// Mod time unchanged, assume file unchanged
			unchangedFiles = append(unchangedFiles, path)
		}

		return nil
	})

	if err != nil {
		idx.setError(fmt.Sprintf("directory walk error: %v", err))
		return err
	}

	// Find deleted files
	var deletedFiles []string
	for path := range existingFiles {
		if !currentFiles[path] {
			deletedFiles = append(deletedFiles, path)
		}
	}

	idx.mu.Lock()
	idx.status.FilesTotal = int64(len(currentFiles))
	idx.status.FilesSkipped = int64(len(unchangedFiles))
	idx.status.FilesDeleted = int64(len(deletedFiles))
	idx.mu.Unlock()

	if progressCb != nil {
		progressCb(idx.GetStatus())
	}

	// Clean up deleted files using atomic operations
	// UpdateFileAtomic now deletes nodes, edges, and metadata atomically
	for _, path := range deletedFiles {
		if err := idx.storage.UpdateFileAtomic(ctx, path, []*graph.CodeNode{}, []*graph.CodeEdge{}); err != nil {
			log.Printf("Warning: failed to delete file %s atomically: %v", path, err)
		}
	}

	// If no changed files, we're done
	if len(changedFiles) == 0 {
		idx.mu.Lock()
		idx.status.State = "idle"
		idx.status.FilesIndexed = 0
		idx.status.CompletedAt = time.Now()
		idx.mu.Unlock()

		if progressCb != nil {
			progressCb(idx.GetStatus())
		}
		return nil
	}

	// Parse all changed files first, storing results per-file
	type fileParseResult struct {
		nodes []parser.CodeNode
		edges []parser.CodeEdge
		err   error
	}
	fileResults := make(map[string]fileParseResult)
	fileNodeCounts := make(map[string]int)
	fileEdgeCounts := make(map[string]int)

	for _, filePath := range changedFiles {
		result, err := idx.parser.ParseFile(ctx, filePath)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v", filePath, err)
			idx.mu.Lock()
			idx.status.Errors = append(idx.status.Errors, fmt.Sprintf("parse error: %s: %v", filePath, err))
			idx.mu.Unlock()
			fileResults[filePath] = fileParseResult{err: err}
			continue
		}

		fileNodeCounts[filePath] = len(result.Nodes)
		fileEdgeCounts[filePath] = len(result.Edges)
		fileResults[filePath] = fileParseResult{
			nodes: result.Nodes,
			edges: result.Edges,
		}
	}

	// Count total nodes across all successfully parsed files
	var totalNodes int
	for _, r := range fileResults {
		if r.err == nil {
			totalNodes += len(r.nodes)
		}
	}

	idx.mu.Lock()
	idx.status.FilesIndexed = int64(len(changedFiles))
	idx.status.NodesTotal = int64(totalNodes)
	idx.mu.Unlock()

	if progressCb != nil {
		progressCb(idx.GetStatus())
	}

	// Process each file atomically: generate embeddings, store, and update metadata
	var nodesProcessed int32

	// Initialize embedding metrics counters
	var retryCount, successCount, failureCount atomic.Int64

	for _, filePath := range changedFiles {
		result := fileResults[filePath]

		// Skip files that failed to parse
		if result.err != nil {
			continue
		}

		// Generate embeddings for this file's nodes
		nodesWithEmbeddings := make([]*graph.CodeNode, 0, len(result.nodes))
		for i := range result.nodes {
			node := &result.nodes[i]
			var emb []float32
			if idx.embedding != nil && node.Content != "" {
				var embErr error
				emb, embErr = retryEmbedding(ctx, idx.embedding, node.ID, node.Content, &retryCount, &successCount, &failureCount)
				if embErr != nil {
					log.Printf("Warning: embedding failed for %s after all retries: %v", node.ID, embErr)
					// Continue without embedding rather than failing entirely
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
			nodesWithEmbeddings = append(nodesWithEmbeddings, graphNode)
		}

		// Convert parser edges to graph edges
		graphEdges := make([]*graph.CodeEdge, 0, len(result.edges))
		for i := range result.edges {
			edge := &result.edges[i]
			graphEdge := &graph.CodeEdge{
				ID:       graph.FormatEdgeID(edge.FromID, edge.ToID, graph.EdgeType(edge.EdgeType)),
				FromID:   edge.FromID,
				ToID:     edge.ToID,
				EdgeType: graph.EdgeType(edge.EdgeType),
				Weight:   1.0,
			}
			graphEdges = append(graphEdges, graphEdge)
		}

		// Atomically update file: delete old nodes/edges and store new ones in a single transaction
		if err := idx.storage.UpdateFileAtomic(ctx, filePath, nodesWithEmbeddings, graphEdges); err != nil {
			log.Printf("Warning: failed to update file %s atomically: %v", filePath, err)
			idx.mu.Lock()
			idx.status.Errors = append(idx.status.Errors, fmt.Sprintf("update error: %s: %v", filePath, err))
			idx.mu.Unlock()
			continue
		}

		// Update file metadata
		info, err := os.Stat(filePath)
		if err != nil {
			log.Printf("Warning: failed to stat %s: %v", filePath, err)
			continue
		}

		hash, err := computeFileHash(ctx, filePath)
		if err != nil {
			log.Printf("Warning: failed to hash %s: %v", filePath, err)
			continue
		}

		lang := idx.parser.DetectLanguage(filePath)
		now := time.Now().Unix()

		meta := &graph.FileMetadata{
			FilePath:    filePath,
			ContentHash: hash,
			ModTime:     info.ModTime().Unix(),
			IndexedAt:   now,
			NodeCount:   fileNodeCounts[filePath],
			EdgeCount:   fileEdgeCounts[filePath],
			FileSize:    info.Size(),
			Language:    string(lang),
		}

		if err := idx.storage.UpsertFileMetadata(ctx, meta); err != nil {
			log.Printf("Warning: failed to save metadata for %s: %v", filePath, err)
		}

		// Update progress
		atomic.AddInt32(&nodesProcessed, int32(len(nodesWithEmbeddings)))
		if progressCb != nil {
			idx.mu.Lock()
			idx.status.NodesCreated = int64(atomic.LoadInt32(&nodesProcessed))
			statusCopy := idx.status
			if statusCopy.Errors != nil {
				statusCopy.Errors = make([]string, len(idx.status.Errors))
				copy(statusCopy.Errors, idx.status.Errors)
			}
			idx.mu.Unlock()
			progressCb(statusCopy)
		}
	}

	// Update final status
	var totalEdges int
	for _, r := range fileResults {
		if r.err == nil {
			totalEdges += len(r.edges)
		}
	}

	idx.mu.Lock()
	idx.status.State = "idle"
	idx.status.EdgesCreated = int64(totalEdges)
	idx.status.EmbeddingSuccessCount = successCount.Load()
	idx.status.EmbeddingRetryCount = retryCount.Load()
	idx.status.EmbeddingFailureCount = failureCount.Load()
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

	// Parse file
	result, err := idx.parser.ParseFile(ctx, absPath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}

	// Initialize embedding metrics counters
	var retryCount, successCount, failureCount atomic.Int64

	// Generate embeddings for all nodes before storing
	nodesWithEmbeddings := make([]*graph.CodeNode, 0, len(result.Nodes))
	for i := range result.Nodes {
		node := &result.Nodes[i]
		var emb []float32
		if idx.embedding != nil && node.Content != "" {
			var embErr error
			emb, embErr = retryEmbedding(ctx, idx.embedding, node.ID, node.Content, &retryCount, &successCount, &failureCount)
			if embErr != nil {
				log.Printf("Warning: embedding failed for %s after all retries: %v", node.ID, embErr)
				// Continue without embedding rather than failing entirely
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
		nodesWithEmbeddings = append(nodesWithEmbeddings, graphNode)
	}

	// Convert parser edges to graph edges
	graphEdges := make([]*graph.CodeEdge, 0, len(result.Edges))
	for i := range result.Edges {
		edge := &result.Edges[i]
		graphEdge := &graph.CodeEdge{
			ID:       graph.FormatEdgeID(edge.FromID, edge.ToID, graph.EdgeType(edge.EdgeType)),
			FromID:   edge.FromID,
			ToID:     edge.ToID,
			EdgeType: graph.EdgeType(edge.EdgeType),
			Weight:   1.0,
		}
		graphEdges = append(graphEdges, graphEdge)
	}

	// Atomically update the file: delete old nodes/edges and store new ones in a single transaction
	if err := idx.storage.UpdateFileAtomic(ctx, absPath, nodesWithEmbeddings, graphEdges); err != nil {
		return fmt.Errorf("atomic file update failed for %s: %w", filePath, err)
	}

	// Log embedding metrics
	if idx.embedding != nil {
		log.Printf("File indexing complete: %d successes, %d retries, %d failures",
			successCount.Load(), retryCount.Load(), failureCount.Load())
	}

	return nil
}

// DeleteFile removes all nodes and edges associated with a file from the index atomically
func (idx *Indexer) DeleteFile(ctx context.Context, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	// Use UpdateFileAtomic with empty nodes/edges to delete atomically
	// This ensures both nodes and their associated edges are deleted together
	return idx.storage.UpdateFileAtomic(ctx, absPath, []*graph.CodeNode{}, []*graph.CodeEdge{})
}

func (idx *Indexer) setError(msg string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.status.State = "error"
	idx.status.LastError = msg
	idx.status.Errors = append(idx.status.Errors, msg)
}
