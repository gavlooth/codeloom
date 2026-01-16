package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/indexer"
	"github.com/heefoo/codeloom/internal/llm"
	"github.com/heefoo/codeloom/internal/parser"
	"github.com/heefoo/codeloom/pkg/mcp"
)

func main() {
	var (
		configPath string
		mode       string
		port       int
		watch      bool
	)

	flag.StringVar(&configPath, "config", "", "Path to config file")
	flag.StringVar(&mode, "mode", "stdio", "Server mode: stdio or http")
	flag.IntVar(&port, "port", 3003, "HTTP server port")
	flag.BoolVar(&watch, "watch", false, "Watch for file changes")
	flag.Parse()

	// Handle subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "start":
			// Parse flags after subcommand
			startCmd := flag.NewFlagSet("start", flag.ExitOnError)
			startCmd.StringVar(&configPath, "config", "", "Path to config file")
			startCmd.IntVar(&port, "port", 3003, "HTTP server port")
			startCmd.BoolVar(&watch, "watch", false, "Watch for file changes")

			if len(os.Args) > 2 {
				mode = os.Args[2]
				if len(os.Args) > 3 {
					startCmd.Parse(os.Args[3:])
				}
			}
		case "index":
			indexCmd(os.Args[2:])
			return
		case "version":
			fmt.Println("codeloom v0.1.0")
			return
		case "help":
			printHelp()
			return
		}
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create LLM provider
	llmProvider, err := llm.NewProvider(cfg.LLM)
	if err != nil {
		log.Fatalf("Failed to create LLM provider: %v", err)
	}

	// Create MCP server
	server := mcp.NewServer(mcp.ServerConfig{
		LLM:    llmProvider,
		Config: cfg,
	})
	defer server.Close()

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Start server
	switch mode {
	case "stdio":
		log.Println("Starting MCP server in stdio mode...")
		if err := server.ServeStdio(ctx); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "http":
		log.Printf("Starting MCP server on port %d...\n", port)
		if err := server.ServeHTTP(ctx, port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf("Unknown mode: %s", mode)
	}
}

func indexCmd(args []string) {
	// Parse index-specific flags
	indexFlags := flag.NewFlagSet("index", flag.ExitOnError)
	configPath := indexFlags.String("config", "", "Path to config file")
	exclude := indexFlags.String("exclude", "", "Comma-separated patterns to exclude")
	noEmbeddings := indexFlags.Bool("no-embeddings", false, "Skip embedding generation")
	verbose := indexFlags.Bool("verbose", false, "Verbose output")

	if err := indexFlags.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	remaining := indexFlags.Args()
	if len(remaining) == 0 {
		fmt.Println("Usage: codeloom index [options] <directory>")
		fmt.Println("\nOptions:")
		indexFlags.PrintDefaults()
		os.Exit(1)
	}

	dir := remaining[0]
	fmt.Printf("Indexing directory: %s\n", dir)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
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
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer storage.Close()

	// Create embedding provider (optional)
	var embProvider embedding.Provider
	if !*noEmbeddings {
		embProvider, err = embedding.NewProvider(cfg.Embedding)
		if err != nil {
			log.Printf("Warning: embedding provider not available: %v", err)
			log.Println("Continuing without embeddings (semantic search will be limited)")
		}
	}

	// Setup exclude patterns
	excludePatterns := indexer.DefaultExcludePatterns()
	if *exclude != "" {
		excludePatterns = append(excludePatterns, strings.Split(*exclude, ",")...)
	}

	// Create indexer
	idx := indexer.New(indexer.Config{
		Parser:          p,
		Storage:         storage,
		Embedding:       embProvider,
		ExcludePatterns: excludePatterns,
	})

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, stopping indexing...")
		cancel()
	}()

	// Progress callback
	progressCb := func(status indexer.Status) {
		if *verbose {
			fmt.Printf("\rProgress: %d/%d nodes, %d edges",
				status.NodesCreated, status.FilesTotal, status.EdgesCreated)
		}
	}

	// Run indexing
	startTime := cfg // reuse variable for timing
	_ = startTime
	fmt.Println("Starting indexing...")

	if err := idx.IndexDirectory(ctx, dir, progressCb); err != nil {
		log.Fatalf("Indexing failed: %v", err)
	}

	// Print final status
	status := idx.GetStatus()
	fmt.Printf("\n\nIndexing complete!\n")
	fmt.Printf("  Directory: %s\n", status.Directory)
	fmt.Printf("  Nodes created: %d\n", status.NodesCreated)
	fmt.Printf("  Edges created: %d\n", status.EdgesCreated)
	fmt.Printf("  Duration: %v\n", status.CompletedAt.Sub(status.StartedAt))

	if len(status.Errors) > 0 {
		fmt.Printf("  Warnings: %d\n", len(status.Errors))
		if *verbose {
			for _, e := range status.Errors {
				fmt.Printf("    - %s\n", e)
			}
		}
	}
}

func printHelp() {
	fmt.Print(`codeloom - Code intelligence MCP server

Commands:
  start <mode>   Start the MCP server (stdio or http)
  index <dir>    Index a codebase directory into the code graph
  version        Show version
  help           Show this help

Index Options:
  --config         Path to config file
  --exclude        Comma-separated patterns to exclude (e.g., "test,mock")
  --no-embeddings  Skip embedding generation (faster, but no semantic search)
  --verbose        Show detailed progress

Server Options:
  --config       Path to config file
  --port         HTTP server port (default: 3003)
  --watch        Watch for file changes

Examples:
  codeloom index ./src                     Index the src directory
  codeloom index --verbose ./              Index current directory with progress
  codeloom index --no-embeddings ./pkg     Index without embeddings (faster)
  codeloom start stdio                     Start MCP server on stdin/stdout
  codeloom start http --port 3003          Start MCP server on HTTP

Environment Variables:
  CODELOOM_LLM_PROVIDER           LLM provider (openai, anthropic, ollama, etc.)
  CODELOOM_MODEL                  Model name
  CODELOOM_OPENAI_COMPATIBLE_URL  Base URL for OpenAI-compatible APIs
  OPENAI_API_KEY                  API key for OpenAI-compatible providers
  ANTHROPIC_API_KEY               API key for Anthropic
  CODELOOM_SURREALDB_URL          SurrealDB connection URL
`)
}
