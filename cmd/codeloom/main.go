package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
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
		watch      bool
	)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "index":
			indexCmd(os.Args[2:])
			return
		case "version":
			fmt.Println("codeloom v0.1.0")
			return
		case "help":
			printHelp()
			return
		case "start":
			startCmd := flag.NewFlagSet("start", flag.ExitOnError)
			var transportFlag stringFlag
			transportFlag.value = "sse"
			var portFlag intFlag
			var httpPathFlag stringFlag

			startCmd.StringVar(&configPath, "config", "", "Path to config file")
			startCmd.Var(&transportFlag, "transport", "Transport: stdio, sse, streamable-http, both, auto")
			startCmd.Var(&portFlag, "port", "HTTP server port")
			startCmd.Var(&httpPathFlag, "http-path", "Streamable HTTP endpoint path")
			startCmd.BoolVar(&watch, "watch", false, "Watch for file changes")

			if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
				transportFlag.value = os.Args[2]
				transportFlag.set = true
				startCmd.Parse(os.Args[3:])
			} else {
				startCmd.Parse(os.Args[2:])
			}

			runServer(configPath, transportFlag, portFlag, httpPathFlag, watch)
			return
		}
	}

	startCmd := flag.NewFlagSet("start", flag.ExitOnError)
	var transportFlag stringFlag
	transportFlag.value = "sse"
	var portFlag intFlag
	var httpPathFlag stringFlag
	startCmd.StringVar(&configPath, "config", "", "Path to config file")
	startCmd.Var(&transportFlag, "transport", "Transport: stdio, sse, streamable-http, both, auto")
	startCmd.Var(&portFlag, "port", "HTTP server port")
	startCmd.Var(&httpPathFlag, "http-path", "Streamable HTTP endpoint path")
	startCmd.BoolVar(&watch, "watch", false, "Watch for file changes")
	startCmd.Parse(os.Args[1:])

	runServer(configPath, transportFlag, portFlag, httpPathFlag, watch)
}

type stringFlag struct {
	value string
	set   bool
}

func (f *stringFlag) String() string {
	return f.value
}

func (f *stringFlag) Set(val string) error {
	f.value = val
	f.set = true
	return nil
}

type intFlag struct {
	value int
	set   bool
}

func (f *intFlag) String() string {
	return fmt.Sprintf("%d", f.value)
}

func (f *intFlag) Set(val string) error {
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("invalid int value %q", val)
	}
	f.value = parsed
	f.set = true
	return nil
}

func runServer(configPath string, transportFlag stringFlag, portFlag intFlag, httpPathFlag stringFlag, watch bool) {
	_ = watch

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	transport := resolveTransport(transportFlag, cfg)
	port := resolvePort(portFlag, cfg)
	httpPath := resolveHTTPPath(httpPathFlag, cfg)

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
	switch transport {
	case "stdio":
		log.Println("Starting MCP server in stdio mode...")
		if err := server.ServeStdio(ctx); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "sse":
		log.Printf("Starting MCP server (SSE) on port %d...\n", port)
		if err := server.ServeSSE(ctx, port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "streamable-http":
		log.Printf("Starting MCP server (Streamable HTTP) on port %d, path %s...\n", port, httpPath)
		if err := server.ServeStreamableHTTP(ctx, port, httpPath); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	case "both":
		log.Printf("Starting MCP server (SSE + Streamable HTTP) on port %d, path %s...\n", port, httpPath)
		if err := server.ServeHTTPMulti(ctx, port, httpPath); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	default:
		log.Fatalf("Unknown transport: %s", transport)
	}
}

func resolveTransport(flagVal stringFlag, cfg *config.Config) string {
	transport := flagVal.value
	sourceDefault := false
	if !flagVal.set {
		if cfg.Server.Transport != "" {
			transport = cfg.Server.Transport
		} else if cfg.Server.Mode != "" {
			transport = cfg.Server.Mode
		} else {
			transport = "sse"
			sourceDefault = true
		}
	}

	transport = normalizeTransport(transport)

	if transport == "auto" {
		return detectTransport()
	}

	if !flagVal.set && sourceDefault && !stdinIsTTY() {
		return "stdio"
	}

	return transport
}

func resolvePort(flagVal intFlag, cfg *config.Config) int {
	if flagVal.set && flagVal.value > 0 {
		return flagVal.value
	}
	if cfg.Server.Port > 0 {
		return cfg.Server.Port
	}
	return 3003
}

func resolveHTTPPath(flagVal stringFlag, cfg *config.Config) string {
	if flagVal.set && flagVal.value != "" {
		return flagVal.value
	}
	if cfg.Server.HTTPPath != "" {
		return cfg.Server.HTTPPath
	}
	return "/mcp"
}

func normalizeTransport(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "", "sse":
		return "sse"
	case "stdio":
		return "stdio"
	case "http":
		return "both"
	case "streamablehttp":
		return "streamable-http"
	case "streamable-http":
		return "streamable-http"
	case "both", "multi":
		return "both"
	case "auto":
		return "auto"
	default:
		return val
	}
}

func detectTransport() string {
	if stdinIsTTY() {
		return "sse"
	}
	return "stdio"
}

func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return (info.Mode() & os.ModeCharDevice) != 0
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

	// Progress callback - always show basic progress
	progressCb := func(status indexer.Status) {
		fmt.Printf("\rProgress: %d files, %d/%d nodes stored, %d edges",
			status.FilesIndexed, status.NodesCreated, status.NodesTotal, status.EdgesCreated)
	}

	// Run indexing
	fmt.Println("Starting indexing...")

	if err := idx.IndexDirectory(ctx, dir, progressCb); err != nil {
		log.Fatalf("Indexing failed: %v", err)
	}

	// Print final status
	status := idx.GetStatus()
	fmt.Printf("\n\nIndexing complete!\n")
	fmt.Printf("  Directory: %s\n", status.Directory)

	// Show incremental vs full index info
	if status.Incremental {
		fmt.Printf("  Mode: incremental\n")
		fmt.Printf("  Files total: %d\n", status.FilesTotal)
		fmt.Printf("  Files changed: %d\n", status.FilesIndexed)
		fmt.Printf("  Files skipped (unchanged): %d\n", status.FilesSkipped)
		if status.FilesDeleted > 0 {
			fmt.Printf("  Files removed: %d\n", status.FilesDeleted)
		}
	} else {
		fmt.Printf("  Mode: full index\n")
		fmt.Printf("  Files indexed: %d\n", status.FilesIndexed)
	}

	fmt.Printf("  Code elements found: %d\n", status.NodesTotal)
	fmt.Printf("  Nodes stored: %d\n", status.NodesCreated)
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
  start          Start the MCP server
  index <dir>    Index a codebase directory into the code graph
  version        Show version
  help           Show this help

Index Options:
  --config         Path to config file
  --exclude        Comma-separated patterns to exclude (e.g., "test,mock")
  --no-embeddings  Skip embedding generation (faster, but no semantic search)
  --verbose        Show detailed errors and warnings

Server Options:
  --config        Path to config file
  --transport     Transport: stdio, sse, streamable-http, both, auto (default: sse)
  --port          HTTP server port (default: from config or 3003)
  --http-path     Streamable HTTP endpoint path (default: from config or /mcp)
  --watch         Watch for file changes

Examples:
  codeloom index ./src                     Index src directory
  codeloom index --verbose ./              Index current directory with detailed errors
  codeloom index --no-embeddings ./pkg     Index without embeddings (faster)
  codeloom start --transport=stdio         Start MCP server on stdin/stdout
  codeloom start --transport=sse           Start MCP server on SSE (http://localhost:3003/sse)
  codeloom start --transport=streamable-http --http-path=/mcp
                                          Start MCP server on Streamable HTTP (http://localhost:3003/mcp)
  codeloom start --transport=both --http-path=/mcp
                                          Start MCP server with SSE + Streamable HTTP on the same port
  codeloom start --transport=auto          Auto-detect transport (stdio if stdin is piped)

Environment Variables:
  CODELOOM_LLM_PROVIDER           LLM provider (openai, anthropic, ollama, etc.)
  CODELOOM_MODEL                  Model name
  CODELOOM_OPENAI_COMPATIBLE_URL  Base URL for OpenAI-compatible APIs
  OPENAI_API_KEY                  API key for OpenAI-compatible providers
  ANTHROPIC_API_KEY               API key for Anthropic
  CODELOOM_SURREALDB_URL          SurrealDB connection URL
  CODELOOM_TRANSPORT              Transport override (stdio, sse, streamable-http, auto)
  CODELOOM_HTTP_PATH              Streamable HTTP path override
`)
}
