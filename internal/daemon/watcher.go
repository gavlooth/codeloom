package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/parser"
)

type Watcher struct {
	watcher         *fsnotify.Watcher
	parser          *parser.Parser
	storage         *graph.Storage
	embedding       embedding.Provider
	excludePatterns []string
	debounceMs      int
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

	return &Watcher{
		watcher:         fsWatcher,
		parser:          cfg.Parser,
		storage:         cfg.Storage,
		embedding:       cfg.Embedding,
		excludePatterns: cfg.ExcludePatterns,
		debounceMs:      debounceMs,
		pendingFiles:    make(map[string]time.Time),
		stopCh:          make(chan struct{}),
	}, nil
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
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		// Also check if pattern matches full path
		if strings.Contains(path, strings.Trim(pattern, "*")) {
			return true
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
		go w.handleDelete(event.Name)
	}
}

func (w *Watcher) queueFile(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pendingFiles[path] = time.Now()
}

func (w *Watcher) processDebounced(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(w.debounceMs) * time.Millisecond)
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
	debounceThreshold := time.Duration(w.debounceMs) * time.Millisecond

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
		if err := w.indexFile(ctx, path); err != nil {
			log.Printf("Failed to index %s: %v", path, err)
		} else {
			log.Printf("Indexed: %s", path)
		}
	}
}

func (w *Watcher) indexFile(ctx context.Context, path string) error {
	// Parse file
	result, err := w.parser.ParseFile(ctx, path)
	if err != nil {
		return err
	}

	// Delete existing nodes for this file
	if err := w.storage.DeleteNodesByFile(ctx, path); err != nil {
		log.Printf("Warning: failed to delete existing nodes: %v", err)
	}

	// Generate embeddings and store nodes
	for _, node := range result.Nodes {
		// Generate embedding for node content (optional)
		var emb []float32
		if w.embedding != nil && node.Content != "" {
			emb, err = w.embedding.EmbedSingle(ctx, node.Content)
			if err != nil {
				log.Printf("Warning: embedding failed for %s: %v", node.ID, err)
				emb = nil // Continue without embedding
			}
		}

		// Convert to graph.CodeNode and store (always, even without embedding)
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
		if err := w.storage.UpsertNode(ctx, graphNode); err != nil {
			log.Printf("Warning: failed to store node %s: %v", node.ID, err)
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
		if err := w.storage.UpsertEdge(ctx, graphEdge); err != nil {
			log.Printf("Warning: failed to store edge: %v", err)
		}
	}

	return nil
}

func (w *Watcher) handleDelete(path string) {
	// Use a timeout context to avoid blocking indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := w.storage.DeleteNodesByFile(ctx, path); err != nil {
		log.Printf("Warning: failed to delete nodes for %s: %v", path, err)
	} else {
		log.Printf("Deleted nodes for: %s", path)
	}
}
