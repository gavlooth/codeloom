package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/daemon"
	"github.com/heefoo/codeloom/internal/embedding"
	"github.com/heefoo/codeloom/internal/graph"
	"github.com/heefoo/codeloom/internal/indexer"
	"github.com/heefoo/codeloom/internal/llm"
	"github.com/heefoo/codeloom/internal/parser"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Server struct {
	llm       llm.Provider
	config    *config.Config
	mcp       *server.MCPServer
	indexer   *indexer.Indexer
	storage   *graph.Storage
	embedding embedding.Provider
	watcher   *daemon.Watcher
	watchCtx  context.Context
	watchStop context.CancelFunc
	watchDirs []string
	mu        sync.RWMutex
}

type ServerConfig struct {
	LLM    llm.Provider
	Config *config.Config
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		llm:    cfg.LLM,
		config: cfg.Config,
	}

	// Create MCP server
	mcpServer := server.NewMCPServer(
		"codeloom",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	s.registerTools(mcpServer)

	s.mcp = mcpServer
	return s
}

// initializeIndexer lazily initializes the indexer and storage
func (s *Server) initializeIndexer() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexer != nil {
		return nil
	}

	// Create storage
	storage, err := graph.NewStorage(graph.StorageConfig{
		URL:       s.config.Database.SurrealDB.URL,
		Namespace: s.config.Database.SurrealDB.Namespace,
		Database:  s.config.Database.SurrealDB.Database,
		Username:  s.config.Database.SurrealDB.Username,
		Password:  s.config.Database.SurrealDB.Password,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	s.storage = storage

	// Create embedding provider (optional)
	embProvider, err := embedding.NewProvider(s.config.Embedding)
	if err != nil {
		log.Printf("Warning: embedding provider not available: %v", err)
	}
	s.embedding = embProvider

	// Create parser and indexer
	p := parser.NewParser()
	s.indexer = indexer.New(indexer.Config{
		Parser:          p,
		Storage:         storage,
		Embedding:       embProvider,
		ExcludePatterns: indexer.DefaultExcludePatterns(),
	})

	return nil
}

func (s *Server) registerTools(mcpServer *server.MCPServer) {
	// ==========================================================================
	// CODEBASE INDEXING TOOLS
	// ==========================================================================

	// codeloom_index tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_index",
		Description: `INDEX SOURCE CODE files into CodeLoom's code graph database.

PURPOSE: Parse and index a codebase directory so that code analysis tools work.
This tool MUST be called before using codeloom_search, codeloom_dependencies, or codeloom_trace.

WHEN TO USE:
- First time analyzing a new codebase
- After major code changes (new files, refactoring)
- When other codeloom_ tools return empty results

NOT FOR: General knowledge storage, notes, or documentation. Use mcp__memory__ tools for that.

Example: {"directory": "./src", "exclude_patterns": ["test", "mock"]}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"directory": map[string]interface{}{
					"type":        "string",
					"description": "Path to the source code directory to index (e.g., './src', '/path/to/project')",
				},
				"exclude_patterns": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Additional patterns to exclude (node_modules, .git, vendor are excluded by default)",
				},
				"skip_embeddings": map[string]interface{}{
					"type":        "boolean",
					"description": "Skip embedding generation for faster indexing (disables semantic search)",
					"default":     false,
				},
			},
			Required: []string{"directory"},
		},
	}, s.handleIndex)

	// codeloom_index_status tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_index_status",
		Description: `Check the status of CodeLoom's source code index.

PURPOSE: See if a codebase has been indexed and get statistics about the code graph.

Returns: state (idle/indexing/error), nodes_created, edges_created, last indexed directory.

Example: {}`,
		InputSchema: mcp.ToolInputSchema{
			Type:       "object",
			Properties: map[string]interface{}{},
		},
	}, s.handleIndexStatus)

	// codeloom_watch tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_watch",
		Description: `Start or stop watching SOURCE CODE files for automatic re-indexing.

PURPOSE: Keep the code graph up-to-date as files change in the codebase.
When watching is enabled, file changes are automatically detected and the index is updated.

WHEN TO USE:
- After initial indexing, to keep the index fresh
- During active development sessions
- "Start watching ./src for changes"
- "Stop watching"

NOT FOR: General file monitoring. This watches SOURCE CODE for index updates only.

Returns: watch status (started/stopped), directories being watched.

Example: {"action": "start", "directories": ["./src", "./pkg"]}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "Action to perform: 'start' to begin watching, 'stop' to stop, 'status' to check",
					"enum":        []string{"start", "stop", "status"},
				},
				"directories": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Directories to watch (required for 'start' action)",
				},
			},
			Required: []string{"action"},
		},
	}, s.handleWatch)

	// ==========================================================================
	// CODE ANALYSIS TOOLS (LLM-powered)
	// ==========================================================================

	// codeloom_context tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_context",
		Description: `Analyze SOURCE CODE to gather context for understanding a programming question.

PURPOSE: Get AI-powered analysis of code structure, patterns, and relationships in THIS CODEBASE.

WHEN TO USE:
- "How does the authentication system work?"
- "What does the UserService class do?"
- "Explain the data flow for order processing"

NOT FOR: Storing memories or creating knowledge graphs. This analyzes SOURCE CODE FILES only.

Returns: summary, analysis, highlights with file:line locations, related code, risks, next steps.

Example: {"query": "How does the payment processing work?"}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Question about the source code (e.g., 'How does authentication work?')",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Analysis focus: 'search' for finding code, 'builder' for implementation, 'question' for understanding",
					"enum":        []string{"search", "builder", "question"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticContext)

	// codeloom_impact tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_impact",
		Description: `Analyze the IMPACT of changing SOURCE CODE in this codebase.

PURPOSE: Understand what breaks or needs updating when you modify specific code.

WHEN TO USE:
- "What happens if I change the User model?"
- "What depends on the authenticate() function?"
- "Impact of removing the deprecated API endpoint"

NOT FOR: General knowledge. This analyzes SOURCE CODE DEPENDENCIES only.

Returns: summary, affected_locations with file:line, risks, recommended steps.

Example: {"query": "What if I rename the DatabaseConnection class?"}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Describe the code change to analyze (e.g., 'Rename UserModel to Account')",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Focus: 'dependencies' for what this code uses, 'call_chain' for what calls this code",
					"enum":        []string{"dependencies", "call_chain"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticImpact)

	// codeloom_architecture tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_architecture",
		Description: `Analyze the ARCHITECTURE of SOURCE CODE in this codebase.

PURPOSE: Understand system structure, modules, layers, and how components connect.

WHEN TO USE:
- "What's the overall structure of this project?"
- "How are the API routes organized?"
- "What's the architecture of the data layer?"

NOT FOR: Creating entity graphs or storing knowledge. This analyzes SOURCE CODE STRUCTURE only.

Returns: summary, architectural highlights, module relationships, file locations.

Example: {"query": "How is the backend API structured?"}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Question about code architecture (e.g., 'How are services organized?')",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Focus: 'structure' for module organization, 'api_surface' for public interfaces",
					"enum":        []string{"structure", "api_surface"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticArchitecture)

	// codeloom_quality tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_quality",
		Description: `Analyze SOURCE CODE QUALITY issues in this codebase.

PURPOSE: Find complexity hotspots, coupling issues, and code quality risks.

WHEN TO USE:
- "What are the most complex functions?"
- "Find tightly coupled modules"
- "Identify code that needs refactoring"

NOT FOR: General notes or documentation. This analyzes SOURCE CODE METRICS only.

Returns: summary, hotspots with file:line, risk notes, improvement suggestions.

Example: {"query": "Find complex functions that need refactoring"}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Quality concern to analyze (e.g., 'Find highly coupled modules')",
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Focus: 'complexity' for complex code, 'coupling' for dependencies, 'hotspots' for frequently changed areas",
					"enum":        []string{"complexity", "coupling", "hotspots"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticQuality)

	// ==========================================================================
	// CODE GRAPH QUERY TOOLS (Database-backed)
	// ==========================================================================

	// codeloom_search tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_search",
		Description: `Search for SOURCE CODE using semantic similarity.

PURPOSE: Find functions, classes, and code snippets by natural language description.
REQUIRES: Run codeloom_index first to populate the code graph.

WHEN TO USE:
- "Find functions that handle user authentication"
- "Search for database connection code"
- "Find error handling patterns"

NOT FOR: Searching memories or knowledge bases. This searches SOURCE CODE FILES only.

Returns: matching code nodes with file paths, line numbers, and content.

Example: {"query": "functions that validate user input", "language": "python"}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language description of code to find",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results to return",
					"default":     10,
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Filter by programming language (go, python, javascript, etc.)",
				},
			},
			Required: []string{"query"},
		},
	}, s.handleSemanticSearch)

	// codeloom_dependencies tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_dependencies",
		Description: `Get transitive dependencies of a SOURCE CODE symbol.

PURPOSE: Find all functions/classes that a piece of code depends on, recursively.
REQUIRES: Run codeloom_index first to populate the code graph.

WHEN TO USE:
- "What does UserService depend on?"
- "Show all imports needed by this module"
- "Get the dependency tree for authenticate()"

NOT FOR: Entity relationships or knowledge graphs. This traces SOURCE CODE IMPORTS/CALLS only.

Returns: list of code nodes that the specified symbol depends on.

Example: {"node_id": "src/services/user.go::UserService", "depth": 3}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "ID of the code node (format: filepath::symbolname)",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum depth to traverse",
					"default":     3,
				},
			},
			Required: []string{"node_id"},
		},
	}, s.handleTransitiveDeps)

	// codeloom_trace tool
	mcpServer.AddTool(mcp.Tool{
		Name: "codeloom_trace",
		Description: `Trace the call chain between two SOURCE CODE functions.

PURPOSE: Find how one function calls another through intermediate functions.
REQUIRES: Run codeloom_index first to populate the code graph.

WHEN TO USE:
- "How does main() reach database.Query()?"
- "Trace the path from HTTP handler to the repository layer"
- "Show the call chain from login to token generation"

NOT FOR: Entity relationships. This traces SOURCE CODE FUNCTION CALLS only.

Returns: the call chain showing each function in the path.

Example: {"from": "main", "to": "database.Query", "max_depth": 10}`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"from": map[string]interface{}{
					"type":        "string",
					"description": "Starting function name or ID",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Target function name or ID",
				},
				"max_depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum depth to search",
					"default":     10,
				},
			},
			Required: []string{"from", "to"},
		},
	}, s.handleTraceCallChain)
}

// ==========================================================================
// TOOL HANDLERS
// ==========================================================================

type AgenticRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Focus string `json:"focus,omitempty"`
}

func (s *Server) handleIndex(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dir, _ := request.Params.Arguments["directory"].(string)
	if dir == "" {
		return errorResult("directory is required")
	}

	// Initialize indexer if needed
	if err := s.initializeIndexer(); err != nil {
		return errorResult(fmt.Sprintf("failed to initialize indexer: %v", err))
	}

	// Parse exclude patterns
	var excludePatterns []string
	if patterns, ok := request.Params.Arguments["exclude_patterns"].([]interface{}); ok {
		for _, p := range patterns {
			if ps, ok := p.(string); ok {
				excludePatterns = append(excludePatterns, ps)
			}
		}
	}

	// Update indexer exclude patterns if provided
	if len(excludePatterns) > 0 {
		allPatterns := append(indexer.DefaultExcludePatterns(), excludePatterns...)
		s.mu.Lock()
		s.indexer = indexer.New(indexer.Config{
			Parser:          parser.NewParser(),
			Storage:         s.storage,
			Embedding:       s.embedding,
			ExcludePatterns: allPatterns,
		})
		s.mu.Unlock()
	}

	// Run indexing with a reasonable timeout
	indexCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := s.indexer.IndexDirectory(indexCtx, dir, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("indexing failed: %v", err))
	}

	status := s.indexer.GetStatus()
	result := map[string]interface{}{
		"success":       true,
		"directory":     status.Directory,
		"nodes_created": status.NodesCreated,
		"edges_created": status.EdgesCreated,
		"duration":      status.CompletedAt.Sub(status.StartedAt).String(),
		"errors_count":  len(status.Errors),
	}

	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func (s *Server) handleIndexStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.indexer == nil {
		result := map[string]interface{}{
			"state":   "not_initialized",
			"message": "Indexer not initialized. Call codeloom_index first to index a codebase.",
		}
		jsonBytes, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(jsonBytes),
				},
			},
		}, nil
	}

	status := s.indexer.GetStatus()
	result := map[string]interface{}{
		"state":         status.State,
		"directory":     status.Directory,
		"files_total":   status.FilesTotal,
		"files_indexed": status.FilesIndexed,
		"nodes_total":   status.NodesTotal,
		"nodes_created": status.NodesCreated,
		"edges_created": status.EdgesCreated,
		"last_error":    status.LastError,
	}

	if !status.StartedAt.IsZero() {
		result["started_at"] = status.StartedAt.Format(time.RFC3339)
	}
	if !status.CompletedAt.IsZero() {
		result["completed_at"] = status.CompletedAt.Format(time.RFC3339)
		result["duration"] = status.CompletedAt.Sub(status.StartedAt).String()
	}

	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func (s *Server) handleAgenticContext(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.llm == nil {
		return errorResult("LLM provider not configured. Set CODELOOM_LLM_PROVIDER and required API keys.")
	}

	req := parseAgenticRequest(request.Params.Arguments)

	// Use a fresh context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get code context from graph if available
	codeContext := s.gatherCodeContext(ctx, req.Query, req.Limit)

	prompt := fmt.Sprintf(`You are a code analysis expert. Analyze the following query about a codebase.

## Query
%s

## Relevant Code from the Codebase
%s

## Instructions
Based on the code above, provide a comprehensive answer to the query.

Provide your analysis in this JSON format:
{
  "summary": "Brief summary answering the query",
  "analysis": "Detailed analysis based on the actual code",
  "highlights": ["Key code locations as file:line references"],
  "related_locations": ["Other relevant files/functions to explore"],
  "risks": ["Potential risks or concerns"],
  "next_steps": ["Recommended actions"],
  "confidence": "high/medium/low"
}`, req.Query, codeContext)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *Server) handleAgenticImpact(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.llm == nil {
		return errorResult("LLM provider not configured. Set CODELOOM_LLM_PROVIDER and required API keys.")
	}

	req := parseAgenticRequest(request.Params.Arguments)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get code context and dependency information
	codeContext := s.gatherCodeContext(ctx, req.Query, req.Limit)
	dependencyContext := s.gatherDependencyContext(ctx, req.Query)

	prompt := fmt.Sprintf(`You are a code impact analysis expert. Analyze the potential impact of changes described in this query.

## Query
%s

## Relevant Code
%s

## Dependency Information
%s

## Instructions
Analyze what would be affected if changes were made based on the query. Consider:
- Direct dependencies (what this code uses)
- Reverse dependencies (what uses this code)
- Potential cascading effects

Provide your analysis in this JSON format:
{
  "summary": "Brief summary of impact",
  "analysis": "Detailed impact analysis based on the actual code",
  "affected_locations": ["Files and locations as file:line that would be affected"],
  "risks": ["Risks of making changes"],
  "next_steps": ["Recommended steps before making changes"],
  "confidence": "high/medium/low"
}`, req.Query, codeContext, dependencyContext)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *Server) handleAgenticArchitecture(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.llm == nil {
		return errorResult("LLM provider not configured. Set CODELOOM_LLM_PROVIDER and required API keys.")
	}

	req := parseAgenticRequest(request.Params.Arguments)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get structural overview of the codebase
	structureContext := s.gatherStructureContext(ctx, req.Query)
	codeContext := s.gatherCodeContext(ctx, req.Query, req.Limit)

	prompt := fmt.Sprintf(`You are a software architecture expert. Analyze the architecture of this codebase.

## Query
%s

## Codebase Structure
%s

## Relevant Code
%s

## Instructions
Analyze the architectural patterns, module organization, and design decisions visible in the code.

Provide your analysis in this JSON format:
{
  "summary": "Brief architectural summary",
  "analysis": "Detailed architectural analysis based on the actual code structure",
  "highlights": ["Key architectural components with file:line references"],
  "related_locations": ["Relevant files and modules"],
  "risks": ["Architectural concerns"],
  "next_steps": ["Recommended architectural improvements"],
  "confidence": "high/medium/low"
}`, req.Query, structureContext, codeContext)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *Server) handleAgenticQuality(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.llm == nil {
		return errorResult("LLM provider not configured. Set CODELOOM_LLM_PROVIDER and required API keys.")
	}

	req := parseAgenticRequest(request.Params.Arguments)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get code context with focus on potential quality issues
	codeContext := s.gatherCodeContext(ctx, req.Query, req.Limit*2) // More samples for quality analysis
	metricsContext := s.gatherMetricsContext(ctx)

	prompt := fmt.Sprintf(`You are a code quality expert. Analyze code quality issues in this codebase.

## Query
%s

## Relevant Code
%s

## Codebase Metrics
%s

## Instructions
Analyze the code for quality issues such as:
- Complexity (long functions, deep nesting)
- Code duplication
- Tight coupling between modules
- Missing error handling
- Poor naming conventions
- Lack of documentation

Provide your analysis in this JSON format:
{
  "summary": "Brief quality summary",
  "analysis": "Detailed quality analysis based on the actual code",
  "hotspots": ["Code quality hotspots with file:line references"],
  "risk_notes": ["Quality risks"],
  "next_steps": ["Specific recommended improvements"],
  "confidence": "high/medium/low"
}`, req.Query, codeContext, metricsContext)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(ctx, messages)
	if err != nil {
		return nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func (s *Server) handleSemanticSearch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := request.Params.Arguments["query"].(string)
	limit := 10
	if l, ok := request.Params.Arguments["limit"].(float64); ok {
		limit = int(l)
	}
	language, _ := request.Params.Arguments["language"].(string)

	// Check if indexer is initialized
	if s.indexer == nil || s.storage == nil {
		return errorResult("Code graph not initialized. Run codeloom_index first to index your codebase.")
	}

	// Check if we have embeddings
	if s.embedding == nil {
		return errorResult("Embedding provider not available. Semantic search requires embeddings. Run codeloom_index with embeddings enabled.")
	}

	// Generate embedding for query
	queryEmb, err := s.embedding.EmbedSingle(ctx, query)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to generate query embedding: %v", err))
	}

	// Search
	nodes, err := s.storage.SemanticSearch(ctx, queryEmb, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("Search failed: %v", err))
	}

	// Filter by language if specified
	var results []map[string]interface{}
	for _, node := range nodes {
		if language != "" && node.Language != language {
			continue
		}
		results = append(results, map[string]interface{}{
			"id":         node.ID,
			"name":       node.Name,
			"type":       node.NodeType,
			"language":   node.Language,
			"file_path":  node.FilePath,
			"start_line": node.StartLine,
			"end_line":   node.EndLine,
			"content":    truncateContent(node.Content, 500),
		})
	}

	result := map[string]interface{}{
		"query":   query,
		"results": results,
		"count":   len(results),
	}

	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func (s *Server) handleTransitiveDeps(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	nodeID, _ := request.Params.Arguments["node_id"].(string)
	depth := 3
	if d, ok := request.Params.Arguments["depth"].(float64); ok {
		depth = int(d)
	}

	// Check if indexer is initialized
	if s.indexer == nil || s.storage == nil {
		return errorResult("Code graph not initialized. Run codeloom_index first to index your codebase.")
	}

	nodes, err := s.storage.GetTransitiveDependencies(ctx, nodeID, depth)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to get dependencies: %v", err))
	}

	var results []map[string]interface{}
	for _, node := range nodes {
		results = append(results, map[string]interface{}{
			"id":        node.ID,
			"name":      node.Name,
			"type":      node.NodeType,
			"file_path": node.FilePath,
			"line":      node.StartLine,
		})
	}

	result := map[string]interface{}{
		"node_id":      nodeID,
		"depth":        depth,
		"dependencies": results,
		"count":        len(results),
	}

	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func (s *Server) handleTraceCallChain(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	from, _ := request.Params.Arguments["from"].(string)
	to, _ := request.Params.Arguments["to"].(string)
	maxDepth := 10
	if d, ok := request.Params.Arguments["max_depth"].(float64); ok {
		maxDepth = int(d)
	}

	// Check if indexer is initialized
	if s.indexer == nil || s.storage == nil {
		return errorResult("Code graph not initialized. Run codeloom_index first to index your codebase.")
	}

	edges, err := s.storage.TraceCallChain(ctx, from, to)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to trace call chain: %v", err))
	}

	var chain []map[string]interface{}
	for _, edge := range edges {
		chain = append(chain, map[string]interface{}{
			"from":      edge.FromID,
			"to":        edge.ToID,
			"edge_type": edge.EdgeType,
		})
	}

	result := map[string]interface{}{
		"from":       from,
		"to":         to,
		"max_depth":  maxDepth,
		"call_chain": chain,
		"found":      len(chain) > 0,
	}

	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
	}, nil
}

func (s *Server) handleWatch(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	action, _ := request.Params.Arguments["action"].(string)

	switch action {
	case "start":
		// Get directories to watch
		var dirs []string
		if d, ok := request.Params.Arguments["directories"].([]interface{}); ok {
			for _, dir := range d {
				if ds, ok := dir.(string); ok {
					dirs = append(dirs, ds)
				}
			}
		}
		if len(dirs) == 0 {
			return errorResult("directories are required for 'start' action")
		}

		// Initialize indexer/storage if needed
		if err := s.initializeIndexer(); err != nil {
			return errorResult(fmt.Sprintf("failed to initialize indexer: %v", err))
		}

		// Stop existing watcher if running
		s.mu.Lock()
		if s.watcher != nil {
			s.watcher.Stop()
			if s.watchStop != nil {
				s.watchStop()
			}
		}

		// Create new watcher
		watcher, err := daemon.NewWatcher(daemon.WatcherConfig{
			Parser:          parser.NewParser(),
			Storage:         s.storage,
			Embedding:       s.embedding,
			ExcludePatterns: indexer.DefaultExcludePatterns(),
			DebounceMs:      s.config.Server.WatcherDebounceMs,
		})
		if err != nil {
			s.mu.Unlock()
			return errorResult(fmt.Sprintf("failed to create watcher: %v", err))
		}

		// Create context for watcher
		watchCtx, watchStop := context.WithCancel(context.Background())
		s.watcher = watcher
		s.watchCtx = watchCtx
		s.watchStop = watchStop
		s.watchDirs = dirs
		s.mu.Unlock()

		// Start watching in background
		go func() {
			if err := watcher.Watch(watchCtx, dirs); err != nil {
				if err != context.Canceled {
					log.Printf("Watcher error: %v", err)
				}
			}
		}()

		result := map[string]interface{}{
			"status":      "started",
			"directories": dirs,
			"message":     fmt.Sprintf("Now watching %d directories for source code changes", len(dirs)),
		}
		jsonBytes, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(jsonBytes),
				},
			},
		}, nil

	case "stop":
		s.mu.Lock()
		if s.watcher == nil {
			s.mu.Unlock()
			return errorResult("No watcher is currently running")
		}

		s.watcher.Stop()
		if s.watchStop != nil {
			s.watchStop()
		}
		watchedDirs := s.watchDirs
		s.watcher = nil
		s.watchCtx = nil
		s.watchStop = nil
		s.watchDirs = nil
		s.mu.Unlock()

		result := map[string]interface{}{
			"status":              "stopped",
			"previously_watching": watchedDirs,
			"message":             "Stopped watching for source code changes",
		}
		jsonBytes, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(jsonBytes),
				},
			},
		}, nil

	case "status":
		s.mu.RLock()
		isWatching := s.watcher != nil
		dirs := s.watchDirs
		s.mu.RUnlock()

		result := map[string]interface{}{
			"watching":    isWatching,
			"directories": dirs,
		}
		if isWatching {
			result["message"] = fmt.Sprintf("Watching %d directories for changes", len(dirs))
		} else {
			result["message"] = "Not currently watching any directories"
		}

		jsonBytes, _ := json.Marshal(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{
					Type: "text",
					Text: string(jsonBytes),
				},
			},
		}, nil

	default:
		return errorResult(fmt.Sprintf("unknown action: %s (valid: start, stop, status)", action))
	}
}

// ==========================================================================
// HELPER FUNCTIONS
// ==========================================================================

func parseAgenticRequest(args map[string]interface{}) AgenticRequest {
	req := AgenticRequest{
		Limit: 5,
	}
	if q, ok := args["query"].(string); ok {
		req.Query = q
	}
	if l, ok := args["limit"].(float64); ok {
		req.Limit = int(l)
	}
	if f, ok := args["focus"].(string); ok {
		req.Focus = f
	}
	return req
}

func errorResult(msg string) (*mcp.CallToolResult, error) {
	result := map[string]interface{}{
		"error":   true,
		"message": msg,
	}
	jsonBytes, _ := json.Marshal(result)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(jsonBytes),
			},
		},
		IsError: true,
	}, nil
}

func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// ==========================================================================
// CODE GRAPH CONTEXT HELPERS
// ==========================================================================

// gatherCodeContext searches for relevant code using semantic search when available,
// or falls back to name-based search when embeddings are disabled
func (s *Server) gatherCodeContext(ctx context.Context, query string, limit int) string {
	if s.storage == nil {
		return "(Code graph not initialized. Run codeloom_index first.)"
	}

	if limit <= 0 {
		limit = 5
	}

	// Try semantic search first if embeddings are available
	if s.embedding != nil {
		// Generate embedding for query
		queryEmb, err := s.embedding.EmbedSingle(ctx, query)
		if err != nil {
			// Fall back to name-based search if embedding fails
			return s.gatherCodeContextByName(ctx, query, limit)
		}

		// Search for relevant code
		nodes, err := s.storage.SemanticSearch(ctx, queryEmb, limit)
		if err != nil {
			// Fall back to name-based search on error
			return s.gatherCodeContextByName(ctx, query, limit)
		}

		if len(nodes) == 0 {
			return "(No relevant code found in indexed codebase.)"
		}

		return s.formatCodeNodes(nodes)
	}

	// No embeddings available - use name-based search
	return s.gatherCodeContextByName(ctx, query, limit)
}

// gatherDependencyContext gets dependency information for impact analysis
// Works with or without embeddings by using name-based search as fallback
func (s *Server) gatherDependencyContext(ctx context.Context, query string) string {
	if s.storage == nil {
		return "(Code graph not initialized.)"
	}

	// Find relevant nodes using semantic search if available, or name-based search otherwise
	var nodes []graph.CodeNode
	var err error

	if s.embedding != nil {
		// Try semantic search first
		queryEmb, embedErr := s.embedding.EmbedSingle(ctx, query)
		if embedErr == nil {
			nodes, err = s.storage.SemanticSearch(ctx, queryEmb, 3)
		}
	}

	// Fall back to name-based search if no embeddings or semantic search failed
	if len(nodes) == 0 {
		potentialNames := s.extractPotentialNames(query)
		for _, name := range potentialNames {
			nameNodes, nameErr := s.storage.FindByName(ctx, name)
			if nameErr == nil {
				nodes = append(nodes, nameNodes...)
			}
			// Limit to 3 total nodes to avoid overwhelming output
			if len(nodes) >= 3 {
				break
			}
		}
	}

	if err != nil && len(nodes) == 0 {
		return "(No dependency information available.)"
	}

	if len(nodes) == 0 {
		return "(No matching code found for dependency analysis. Semantic search is not available - embeddings are disabled.)"
	}

	var sb strings.Builder
	sb.WriteString("### Dependencies Analysis\n\n")

	for _, node := range nodes {
		// Get dependencies (what this node uses)
		deps, err := s.storage.GetTransitiveDependencies(ctx, node.ID, 2)
		if err == nil && len(deps) > 0 {
			sb.WriteString(fmt.Sprintf("**%s** depends on:\n", node.Name))
			for _, dep := range deps {
				sb.WriteString(fmt.Sprintf("  - %s (%s) at %s:%d\n", dep.Name, dep.NodeType, dep.FilePath, dep.StartLine))
			}
			sb.WriteString("\n")
		}

		// Get callers (what calls this node)
		callers, err := s.storage.GetCallers(ctx, node.ID)
		if err == nil && len(callers) > 0 {
			sb.WriteString(fmt.Sprintf("**%s** is called by:\n", node.Name))
			for _, caller := range callers {
				sb.WriteString(fmt.Sprintf("  - %s (%s) at %s:%d\n", caller.Name, caller.NodeType, caller.FilePath, caller.StartLine))
			}
			sb.WriteString("\n")
		}
	}

	if sb.Len() == len("### Dependencies Analysis\n\n") {
		return "(No dependency relationships found for the matching code.)"
	}
	return sb.String()
}

// gatherStructureContext provides an overview of the codebase structure
func (s *Server) gatherStructureContext(ctx context.Context, query string) string {
	if s.storage == nil {
		return "(Code graph not initialized.)"
	}

	// Get all nodes to analyze structure
	allNodes, err := s.storage.GetAllNodes(ctx)
	if err != nil {
		return fmt.Sprintf("(Failed to get structure: %v)", err)
	}

	if len(allNodes) == 0 {
		return "(No code indexed yet.)"
	}

	// Count by type and language
	typeCount := make(map[string]int)
	langCount := make(map[string]int)
	fileCount := make(map[string]bool)

	for _, node := range allNodes {
		typeCount[string(node.NodeType)]++
		langCount[node.Language]++
		fileCount[node.FilePath] = true
	}

	var sb strings.Builder
	sb.WriteString("### Codebase Overview\n\n")
	sb.WriteString(fmt.Sprintf("**Total files indexed:** %d\n", len(fileCount)))
	sb.WriteString(fmt.Sprintf("**Total code elements:** %d\n\n", len(allNodes)))

	sb.WriteString("**By Type:**\n")
	for t, count := range typeCount {
		sb.WriteString(fmt.Sprintf("  - %s: %d\n", t, count))
	}

	sb.WriteString("\n**By Language:**\n")
	for l, count := range langCount {
		if l != "" {
			sb.WriteString(fmt.Sprintf("  - %s: %d\n", l, count))
		}
	}

	return sb.String()
}

// gatherMetricsContext provides basic code metrics
func (s *Server) gatherMetricsContext(ctx context.Context) string {
	if s.storage == nil {
		return "(Code graph not initialized.)"
	}

	allNodes, err := s.storage.GetAllNodes(ctx)
	if err != nil {
		return ""
	}

	if len(allNodes) == 0 {
		return "(No code indexed.)"
	}

	// Calculate basic metrics
	var totalLines int
	var longestFunction string
	var longestFunctionLines int
	var largeFunction []string

	for _, node := range allNodes {
		lines := node.EndLine - node.StartLine + 1
		totalLines += lines

		if node.NodeType == "function" || node.NodeType == "method" {
			if lines > longestFunctionLines {
				longestFunctionLines = lines
				longestFunction = fmt.Sprintf("%s (%s:%d)", node.Name, node.FilePath, node.StartLine)
			}
			if lines > 50 {
				largeFunction = append(largeFunction, fmt.Sprintf("%s (%d lines) at %s:%d", node.Name, lines, node.FilePath, node.StartLine))
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("### Code Metrics\n\n")
	sb.WriteString(fmt.Sprintf("**Total code elements:** %d\n", len(allNodes)))
	sb.WriteString(fmt.Sprintf("**Total lines:** ~%d\n", totalLines))

	if longestFunction != "" {
		sb.WriteString(fmt.Sprintf("**Longest function:** %s (%d lines)\n", longestFunction, longestFunctionLines))
	}

	if len(largeFunction) > 0 {
		sb.WriteString("\n**Large functions (>50 lines):**\n")
		for _, f := range largeFunction {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	return sb.String()
}

// ==========================================================================
// SERVER METHODS
// ==========================================================================

func (s *Server) ServeStdio(ctx context.Context) error {
	log.Println("Starting MCP server on stdio...")
	return server.ServeStdio(s.mcp)
}

func (s *Server) ServeHTTP(ctx context.Context, port int) error {
	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting MCP server on http://localhost%s\n", addr)

	// Create SSE handler - it handles both /sse (GET) and /message (POST) internally
	sseHandler := server.NewSSEServer(s.mcp,
		server.WithBaseURL(fmt.Sprintf("http://127.0.0.1:%d", port)),
	)

	mux := http.NewServeMux()
	// Mount SSEServer at root so it can handle /sse and /message paths
	mux.Handle("/", sseHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Close cleans up resources
func (s *Server) Close() error {
	var errs []error

	// Stop watcher if running
	s.mu.Lock()
	if s.watcher != nil {
		s.watcher.Stop()
		if s.watchStop != nil {
			s.watchStop()
		}
		s.watcher = nil
		s.watchStop = nil
	}
	s.mu.Unlock()

	// Close LLM provider
	if s.llm != nil {
		if err := s.llm.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close LLM provider: %w", err))
		}
	}

	// Close storage
	if s.storage != nil {
		if err := s.storage.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close storage: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// gatherCodeContextByName searches for code by name (embedding-agnostic fallback)
func (s *Server) gatherCodeContextByName(ctx context.Context, query string, limit int) string {
	// Extract potential function/class names from query
	// This is a simple heuristic - look for words that might be identifiers
	potentialNames := s.extractPotentialNames(query)

	var allNodes []graph.CodeNode
	for _, name := range potentialNames {
		// Search for nodes matching this name
		nodes, err := s.storage.FindByName(ctx, name)
		if err != nil {
			continue
		}
		allNodes = append(allNodes, nodes...)
	}

	// Deduplicate by ID
	seen := make(map[string]bool)
	var uniqueNodes []graph.CodeNode
	for _, node := range allNodes {
		if !seen[node.ID] {
			seen[node.ID] = true
			uniqueNodes = append(uniqueNodes, node)
		}
	}

	if len(uniqueNodes) == 0 {
		return "(No matching code found by name. Semantic search is not available - embeddings are disabled. Re-index with embeddings enabled for better search results.)"
	}

	// Limit results
	if len(uniqueNodes) > limit {
		uniqueNodes = uniqueNodes[:limit]
	}

	return s.formatCodeNodes(uniqueNodes)
}

// extractPotentialNames extracts potential identifier names from a query string
func (s *Server) extractPotentialNames(query string) []string {
	// Simple heuristic: split query and look for capitalized words or camelCase
	// This is a basic implementation that can be improved
	var names []string
	words := strings.Fields(query)
	for _, word := range words {
		// Skip very short words and common non-identifier words
		if len(word) < 3 || strings.ContainsAny(word, ".,!?()[]{};:\\\"'") {
			continue
		}
		// Check if it looks like an identifier (has uppercase or camelCase)
		if strings.ContainsAny(word, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.Contains(word, "_") {
			names = append(names, strings.Trim(word, ",.!?()[]{};:\\\"'"))
		}
	}
	return names
}

// formatCodeNodes formats code nodes for display
func (s *Server) formatCodeNodes(nodes []graph.CodeNode) string {
	var sb strings.Builder
	for i, node := range nodes {
		sb.WriteString(fmt.Sprintf("### %d. %s (%s)\n", i+1, node.Name, node.NodeType))
		sb.WriteString(fmt.Sprintf("File: %s:%d-%d\n", node.FilePath, node.StartLine, node.EndLine))
		sb.WriteString(fmt.Sprintf("Language: %s\n", node.Language))
		if node.DocComment != "" {
			sb.WriteString(fmt.Sprintf("Doc: %s\n", truncateContent(node.DocComment, 200)))
		}
		sb.WriteString("```\n")
		sb.WriteString(truncateContent(node.Content, 1000))
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}
