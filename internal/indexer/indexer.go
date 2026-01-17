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
	"time"

	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
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

// fileInfo holds information about a file for change detection
type fileInfo struct {
	Path    string
	ModTime int64
	Hash    string
}

// computeFileHash computes SHA256 hash of file content
func computeFileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
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
				if matched, _ := filepath.Match(pattern, info.Name()); matched {
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
			hash, err := computeFileHash(path)
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
	for _, path := range deletedFiles {
		// Use UpdateFileAtomic with empty nodes/edges to delete atomically
		if err := idx.storage.UpdateFileAtomic(ctx, path, []*graph.CodeNode{}, []*graph.CodeEdge{}); err != nil {
			log.Printf("Warning: failed to delete file %s atomically: %v", path, err)
			continue
		}
		if err := idx.storage.DeleteFileMetadata(ctx, path); err != nil {
			log.Printf("Warning: failed to delete metadata for %s: %v", path, err)
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

	// Clean up changed files before re-indexing using atomic operations
	for _, path := range changedFiles {
		// Use UpdateFileAtomic with empty nodes/edges to delete atomically
		if err := idx.storage.UpdateFileAtomic(ctx, path, []*graph.CodeNode{}, []*graph.CodeEdge{}); err != nil {
			log.Printf("Warning: failed to delete file %s atomically: %v", path, err)
			continue
		}
	}

	// Parse only the changed files
	var allNodes []parser.CodeNode
	var allEdges []parser.CodeEdge
	fileNodeCounts := make(map[string]int)
	fileEdgeCounts := make(map[string]int)

	for _, filePath := range changedFiles {
		result, err := idx.parser.ParseFile(ctx, filePath)
		if err != nil {
			log.Printf("Warning: failed to parse %s: %v", filePath, err)
			idx.mu.Lock()
			idx.status.Errors = append(idx.status.Errors, fmt.Sprintf("parse error: %s: %v", filePath, err))
			idx.mu.Unlock()
			continue
		}

		allNodes = append(allNodes, result.Nodes...)
		allEdges = append(allEdges, result.Edges...)
		fileNodeCounts[filePath] = len(result.Nodes)
		fileEdgeCounts[filePath] = len(result.Edges)
	}

	idx.mu.Lock()
	idx.status.FilesIndexed = int64(len(changedFiles))
	idx.status.NodesTotal = int64(len(allNodes))
	idx.mu.Unlock()

	if progressCb != nil {
		progressCb(idx.GetStatus())
	}

	// Store nodes using batch embedding
	progressFunc := func(count int) {
		idx.mu.Lock()
		idx.status.NodesCreated = int64(count)

		// Create a deep copy of Status to avoid race conditions.
		// Struct assignment performs a shallow copy, handling all value types automatically.
		// Only the Errors slice (reference type) needs explicit copying.
		statusCopy := idx.status
		if statusCopy.Errors != nil {
			statusCopy.Errors = make([]string, len(idx.status.Errors))
			copy(statusCopy.Errors, idx.status.Errors)
		}

		idx.mu.Unlock()
		if progressCb != nil {
			progressCb(statusCopy)
		}
	}

	if err := StoreNodesBatch(ctx, allNodes, idx.storage, idx.embedding, progressFunc); err != nil {
		idx.setError(fmt.Sprintf("failed to store nodes: %v", err))
		return err
	}

	// Store edges
	if err := StoreEdgesBatch(ctx, allEdges, idx.storage); err != nil {
		idx.setError(err.Error())
		return err
	}

	// Update file metadata for changed files
	now := time.Now().Unix()
	for _, filePath := range changedFiles {
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}

		hash, err := computeFileHash(filePath)
		if err != nil {
			continue
		}

		// Detect language for metadata
		lang := idx.parser.DetectLanguage(filePath)

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
	}

	// Update final status
	idx.mu.Lock()
	idx.status.State = "idle"
	idx.status.NodesCreated = int64(len(allNodes))
	idx.status.EdgesCreated = int64(len(allEdges))
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
