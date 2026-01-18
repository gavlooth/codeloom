package mcp

import (
	"context"
	"testing"

	"github.com/heefoo/codeloom/internal/llm"
)

// Test extractPotentialNames helper function that enables degraded mode
func TestExtractPotentialNames(t *testing.T) {
	testCases := []struct {
		name     string
		query    string
		minCount int
		maxCount int
	}{
		{
			name:     "Extracts class name",
			query:    "What does UserService do?",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "Extracts function name",
			query:    "How does authenticate function work?",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "Extracts multiple identifiers",
			query:    "UserService calls PaymentProcessor",
			minCount: 2,
			maxCount: 3,
		},
		{
			name:     "Handles camel case",
			query:    "Find PaymentProcessor class",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "Handles underscore names",
			query:    "Find user_auth function",
			minCount: 1,
			maxCount: 2,
		},
		{
			name:     "Ignores common lowercase words",
			query:    "How is the function working",
			minCount: 0,
			maxCount: 1,
		},
		{
			name:     "Filters punctuation",
			query:    "Find User.Service, how does it work?",
			minCount: 1,
			maxCount: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{}
			names := server.extractPotentialNames(tc.query)

			if len(names) < tc.minCount {
				t.Errorf("extractPotentialNames(%q) returned %d names, want at least %d",
					tc.query, len(names), tc.minCount)
			}
			if len(names) > tc.maxCount {
				t.Errorf("extractPotentialNames(%q) returned %d names, want at most %d",
					tc.query, len(names), tc.maxCount)
			}
			t.Logf("Extracted names: %v", names)
		})
	}
}

// Test that Server struct properly handles nil embedding provider
func TestServerNilEmbedding(t *testing.T) {
	// Create server config without LLM/embedding
	cfg := ServerConfig{
		LLM:    &mockLLM{},
		Config:  nil,
	}

	server := NewServer(cfg)

	// Verify embedding can be nil without causing panics
	if server.embedding != nil {
		t.Error("Server should allow nil embedding provider for degraded mode")
	}

	t.Log("Server correctly handles nil embedding provider")
}

type mockLLM struct{}

func (m *mockLLM) Generate(ctx context.Context, messages []llm.Message, opts ...llm.Option) (string, error) {
	return "", nil
}

func (m *mockLLM) GenerateWithTools(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}

func (m *mockLLM) Stream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (<-chan string, error) {
	return nil, nil
}

func (m *mockLLM) Name() string {
	return "mock"
}

func (m *mockLLM) Close() error {
	return nil
}
