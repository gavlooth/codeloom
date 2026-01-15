package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/llm"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Server struct {
	llm    llm.Provider
	config *config.Config
	mcp    *server.MCPServer
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

func (s *Server) registerTools(mcpServer *server.MCPServer) {
	// agentic_context tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "agentic_context",
		Description: "Gather client-readable context for a query. Returns JSON with: summary, analysis, highlights (with file:line and snippets), related_locations, risks, next_steps, and confidence.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query for semantic analysis",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results to return",
					"default":     5,
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus: search, builder, or question",
					"enum":        []string{"search", "builder", "question"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticContext)

	// agentic_impact tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "agentic_impact",
		Description: "Assess change impact for a query. Returns JSON with: summary, analysis, impact highlights, affected locations, risks, next_steps, and confidence.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query for impact analysis",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     5,
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus: dependencies or call_chain",
					"enum":        []string{"dependencies", "call_chain"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticImpact)

	// agentic_architecture tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "agentic_architecture",
		Description: "Summarize system structure relevant to a query. Returns JSON with: summary, analysis, highlights, related_locations, risks, next_steps, and confidence.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query for architecture analysis",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     5,
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus: structure or api_surface",
					"enum":        []string{"structure", "api_surface"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticArchitecture)

	// agentic_quality tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "agentic_quality",
		Description: "Highlight quality risks related to a query. Returns JSON with: summary, analysis, hotspot highlights, risk notes, next_steps, and confidence.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query for quality analysis",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     5,
				},
				"focus": map[string]interface{}{
					"type":        "string",
					"description": "Optional focus: complexity, coupling, or hotspots",
					"enum":        []string{"complexity", "coupling", "hotspots"},
				},
			},
			Required: []string{"query"},
		},
	}, s.handleAgenticQuality)

	// semantic_code_search tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "semantic_code_search",
		Description: "Search for code using semantic similarity. Returns matching code nodes with file paths and content.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Natural language search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     10,
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Filter by programming language",
				},
			},
			Required: []string{"query"},
		},
	}, s.handleSemanticSearch)

	// get_transitive_dependencies tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "get_transitive_dependencies",
		Description: "Get all transitive dependencies of a code node up to a specified depth.",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]interface{}{
				"node_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the node to analyze",
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

	// trace_call_chain tool
	mcpServer.AddTool(mcp.Tool{
		Name:        "trace_call_chain",
		Description: "Trace the call chain between two functions.",
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

// Tool handlers

type AgenticRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
	Focus string `json:"focus,omitempty"`
}

func (s *Server) handleAgenticContext(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	req := parseAgenticRequest(request.Params.Arguments)

	// Use a fresh context with timeout to avoid MCP request context cancellation
	llmCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Analyze the following query and provide context for understanding it in a codebase:

Query: %s

Provide your analysis in this JSON format:
{
  "summary": "Brief summary of the context",
  "analysis": "How this answers the query",
  "highlights": ["Key points with file:line references if applicable"],
  "related_locations": ["Related code locations"],
  "risks": ["Potential risks or concerns"],
  "next_steps": ["Recommended actions"],
  "confidence": "high/medium/low"
}`, req.Query)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(llmCtx, messages)
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
	req := parseAgenticRequest(request.Params.Arguments)

	llmCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Analyze the potential impact of changes related to this query:

Query: %s

Provide your analysis in this JSON format:
{
  "summary": "Brief summary of impact",
  "analysis": "Impact analysis details",
  "affected_locations": ["Files and locations that would be affected"],
  "risks": ["Risks of making changes"],
  "next_steps": ["Recommended steps before making changes"],
  "confidence": "high/medium/low"
}`, req.Query)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(llmCtx, messages)
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
	req := parseAgenticRequest(request.Params.Arguments)

	llmCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Analyze the architecture related to this query:

Query: %s

Provide your analysis in this JSON format:
{
  "summary": "Brief architectural summary",
  "analysis": "How the architecture addresses this query",
  "highlights": ["Key architectural components"],
  "related_locations": ["Relevant files and modules"],
  "risks": ["Architectural concerns"],
  "next_steps": ["Recommended architectural actions"],
  "confidence": "high/medium/low"
}`, req.Query)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(llmCtx, messages)
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
	req := parseAgenticRequest(request.Params.Arguments)

	llmCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Analyze code quality issues related to this query:

Query: %s

Provide your analysis in this JSON format:
{
  "summary": "Brief quality summary",
  "analysis": "Quality analysis details",
  "hotspots": ["Code quality hotspots"],
  "risk_notes": ["Quality risks"],
  "next_steps": ["Recommended quality improvements"],
  "confidence": "high/medium/low"
}`, req.Query)

	messages := []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := s.llm.Generate(llmCtx, messages)
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

	result := map[string]interface{}{
		"query":   query,
		"results": []interface{}{},
		"message": "Semantic search not yet implemented - requires embedding provider and database",
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

	result := map[string]interface{}{
		"node_id":      nodeID,
		"dependencies": []interface{}{},
		"message":      "Transitive dependencies not yet implemented - requires database",
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

	result := map[string]interface{}{
		"from":       from,
		"to":         to,
		"call_chain": []interface{}{},
		"message":    "Call chain tracing not yet implemented - requires database",
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
