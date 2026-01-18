package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
	"github.com/heefoo/codeloom/internal/util"
)

type Watcher struct {
	watcher         *fsnotify.Watcher
	parser          *parser.Parser
	storage         *graph.Storage
	embedding       embedding.Provider
	excludePatterns []string
	debounceMs      atomic.Int64
	indexTimeoutMs  atomic.Int64
	mu              sync.Mutex
	pendingFiles    map[string]time.Time
	stopCh          chan struct{}
	stopOnce        sync.Once
}

type WatcherConfig struct {
	Parser          *parser.Parser
	Storage         *graph.Storage
	Embedding       embedding.Provider
	ExcludePatterns []string
	DebounceMs      int
	IndexTimeoutMs  int
}

func NewWatcher(cfg WatcherConfig) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	debounceMs := cfg.DebounceMs
	if debounceMs == 0 {
		debounceMs = 100 // Default 100ms debounce
	}

	indexTimeoutMs := cfg.IndexTimeoutMs
	if indexTimeoutMs == 0 {
		indexTimeoutMs = 60000 // Default 60 second timeout for indexing operations
	}

	w := &Watcher{
		watcher:         fsWatcher,
		parser:          cfg.Parser,
		storage:         cfg.Storage,
		embedding:       cfg.Embedding,
		excludePatterns: cfg.ExcludePatterns,
		pendingFiles:    make(map[string]time.Time),
		stopCh:          make(chan struct{}),
	}
	w.debounceMs.Store(int64(debounceMs))
	w.indexTimeoutMs.Store(int64(indexTimeoutMs))
	return w, nil
}

func (w *Watcher) Watch(ctx context.Context, dirs []string) error {
	// Add directories to watch
	for _, dir := range dirs {
		if err := w.addDirRecursive(dir); err != nil {
			log.Printf("Warning: failed to watch %s: %v", dir, err)
		}
	}

	// Start debounce processor
	go w.processDebounced(ctx)

	// Start event loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.watcher.Close()
	})
}

func (w *Watcher) addDirRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Check exclude patterns
			if w.shouldExclude(path) {
				return filepath.SkipDir
			}
			return w.watcher.Add(path)
		}
		return nil
	})
}

func (w *Watcher) shouldExclude(path string) bool {
	name := filepath.Base(path)
	for _, pattern := range w.excludePatterns {
		// Check if pattern matches the filename/base directory
		if util.MatchPattern(pattern, name) {
			return true
		}
		// Check if pattern matches any path component
		// Walk up the path to check each directory/file name
		currentPath := path
		for currentPath != "." && currentPath != "/" {
			base := filepath.Base(currentPath)
			if util.MatchPattern(pattern, base) {
				return true
			}
			currentPath = filepath.Dir(currentPath)
		}
	}
	return false
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Skip excluded paths
	if w.shouldExclude(event.Name) {
		return
	}

	// Skip non-code files
	if w.parser.DetectLanguage(event.Name) == "" {
		return
	}

	switch {
	case event.Op&fsnotify.Write == fsnotify.Write,
		event.Op&fsnotify.Create == fsnotify.Create:
		w.queueFile(event.Name)

	case event.Op&fsnotify.Remove == fsnotify.Remove,
		event.Op&fsnotify.Rename == fsnotify.Rename:
		// Handle deletion
		w.queueFile(event.Name + "|DELETE")
	}
}

func (w *Watcher) queueFile(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pendingFiles[path] = time.Now()
}

func (w *Watcher) processDebounced(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(w.debounceMs.Load()) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.processPending(ctx)
		}
	}
}

func (w *Watcher) processPending(ctx context.Context) {
	w.mu.Lock()
	now := time.Now()
	debounceThreshold := time.Duration(w.debounceMs.Load()) * time.Millisecond

	var toProcess []string
	for path, queuedAt := range w.pendingFiles {
		if now.Sub(queuedAt) >= debounceThreshold {
			toProcess = append(toProcess, path)
			delete(w.pendingFiles, path)
		}
	}
	w.mu.Unlock()

	// Process files
	for _, path := range toProcess {
		if strings.HasSuffix(path, "|DELETE") {
			w.handleDelete(ctx, strings.TrimSuffix(path, "|DELETE"))
		} else {
			if err := w.indexFile(ctx, path); err != nil {
				log.Printf("Failed to index %s: %v", path, err)
			} else {
				log.Printf("Indexed: %s", path)
			}
		}
	}
}

func (w *Watcher) indexFile(ctx context.Context, path string) error {
	// Check for context cancellation before starting work
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Parse file
	result, err := w.parser.ParseFile(ctx, path)
	if err != nil {
		return err
	}

	// Generate embeddings for all nodes before the transaction
	// This is done outside the transaction since embedding generation is I/O intensive
	nodesWithEmbeddings := make([]*graph.CodeNode, 0, len(result.Nodes))
	for i := range result.Nodes {
		// Check for cancellation between nodes to avoid processing all if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		node := &result.Nodes[i]
		// Generate embedding for this node
		var emb []float32
		var err error
		if w.embedding != nil && node.Content != "" {
			emb, err = w.embedding.EmbedSingle(ctx, node.Content)
			if err != nil {
				log.Printf("Warning: failed to generate embedding for %s: %v", node.Name, err)
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
	if w.storage != nil {
		if err := w.storage.UpdateFileAtomic(ctx, path, nodesWithEmbeddings, graphEdges); err != nil {
			return fmt.Errorf("atomic file update failed for %s: %w", path, err)
		}
	}

	return nil
}

func (w *Watcher) handleDelete(ctx context.Context, path string) {
	// Use a timeout context to avoid blocking indefinitely
	// This matches the timeout protection used in indexFile
	indexCtx, cancel := context.WithTimeout(ctx, time.Duration(w.indexTimeoutMs.Load())*time.Millisecond)
	defer cancel()

	if path == "" {
		log.Printf("Warning: skipping delete with empty path")
		return
	}

	// Only attempt to delete if storage is configured
	if w.storage != nil {
		if err := w.storage.UpdateFileAtomic(indexCtx, path, []*graph.CodeNode{}, []*graph.CodeEdge{}); err != nil {
			log.Printf("Warning: failed to delete file %s atomically: %v", path, err)
		} else {
			log.Printf("Deleted: %s", path)
		}
	}
}
