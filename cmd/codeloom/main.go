package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/llm"
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
	if len(args) == 0 {
		fmt.Println("Usage: codeloom index <directory>")
		os.Exit(1)
	}
	dir := args[0]
	fmt.Printf("Indexing directory: %s\n", dir)
	// TODO: Implement indexing
}

func printHelp() {
	fmt.Println(`codeloom - Code intelligence MCP server

Commands:
  start <mode>   Start the MCP server (stdio or http)
  index <dir>    Index a directory
  version        Show version
  help           Show this help

Options:
  --config       Path to config file
  --port         HTTP server port (default: 3003)
  --watch        Watch for file changes

Environment Variables:
  CODELOOM_LLM_PROVIDER           LLM provider (openai, anthropic, ollama, etc.)
  CODELOOM_MODEL                  Model name
  CODELOOM_OPENAI_COMPATIBLE_URL  Base URL for OpenAI-compatible APIs
  OPENAI_API_KEY                   API key for OpenAI-compatible providers
  ANTHROPIC_API_KEY                API key for Anthropic
  CODELOOM_SURREALDB_URL          SurrealDB connection URL
`)
}
