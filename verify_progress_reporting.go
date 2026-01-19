package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/indexer"
	"github.com/heefoo/codeloom/internal/parser"
	"github.com/heefoo/codeloom/internal/graph"
)

func main() {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "codeloom_test_*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test Go file
	testFile := filepath.Join(tmpDir, "test.go")
	testContent := `package main

func add(a, b int) int {
	return a + b
}

func subtract(a, b int) int {
	return a - b
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write test file: %v\n", err)
		os.Exit(1)
	}

	// Load config
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create parser
	p := parser.NewParser()

	// Create storage
	storage, err := graph.NewStorage(graph.StorageConfig{
		URL:       cfg.Database.SurrealDB.URL,
		Namespace: cfg.Database.SurrealDB.Namespace,
		Database:  cfg.Database.SurrealDB.Database,
		Username:  cfg.Database.SurrealDB.Username,
		Password:  cfg.Database.SurrealDB.Password,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	// Create indexer
	idx := indexer.New(indexer.Config{
		Parser:          p,
		Storage:         storage,
		Embedding:       nil, // No embeddings for faster test
		ExcludePatterns: indexer.DefaultExcludePatterns(),
	})

	// Track progress callback invocations
	progressCalls := 0
	var lastStatus indexer.Status

	// Progress callback - should always be called regardless of verbose flag
	progressCb := func(status indexer.Status) {
		progressCalls++
		lastStatus = status
		fmt.Printf("\rProgress callback called: %d files, %d/%d nodes stored, %d edges\n",
			status.FilesIndexed, status.NodesCreated, status.NodesTotal, status.EdgesCreated)
	}

	// Create context
	ctx := context.Background()

	// Run indexing
	fmt.Println("Starting progress reporting test...")
	if err := idx.IndexDirectory(ctx, tmpDir, progressCb); err != nil {
		fmt.Fprintf(os.Stderr, "Indexing failed: %v\n", err)
		os.Exit(1)
	}

	// Verify results
	fmt.Println("\n=== Verification Results ===")
	fmt.Printf("Progress callback invoked: %d times\n", progressCalls)
	fmt.Printf("Files indexed: %d\n", lastStatus.FilesIndexed)
	fmt.Printf("Nodes created: %d\n", lastStatus.NodesCreated)
	fmt.Printf("Nodes total: %d\n", lastStatus.NodesTotal)

	if progressCalls == 0 {
		fmt.Println("❌ FAIL: Progress callback was never called")
		os.Exit(1)
	}

	if lastStatus.FilesIndexed == 0 {
		fmt.Println("❌ FAIL: No files were indexed")
		os.Exit(1)
	}

	if lastStatus.NodesCreated == 0 {
		fmt.Println("❌ FAIL: No nodes were created")
		os.Exit(1)
	}

	fmt.Println("✅ PASS: Progress reporting is working correctly!")
}
