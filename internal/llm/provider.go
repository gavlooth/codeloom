package llm

import (
	"context"
	"fmt"

	"github.com/heefoo/codeloom/internal/config"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type GenerateOptions struct {
	Temperature   float32
	MaxTokens     int
	StopSequences []string
	Tools         []Tool
}

type Option func(*GenerateOptions)

func WithTemperature(t float32) Option {
	return func(o *GenerateOptions) {
		o.Temperature = t
	}
}

func WithMaxTokens(n int) Option {
	return func(o *GenerateOptions) {
		o.MaxTokens = n
	}
}

func WithTools(tools []Tool) Option {
	return func(o *GenerateOptions) {
		o.Tools = tools
	}
}

type Provider interface {
	Generate(ctx context.Context, messages []Message, opts ...Option) (string, error)
	GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error)
	Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error)
	Name() string
}

type ToolCallResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

func NewProvider(cfg config.LLMConfig) (Provider, error) {
	switch cfg.Provider {
	case "openai", "openai-compatible":
		return NewOpenAIProvider(cfg)
	case "anthropic":
		return NewAnthropicProvider(cfg)
	case "ollama":
		return NewOllamaProvider(cfg)
	case "google":
		return NewGoogleProvider(cfg)
	case "xai":
		return NewXAIProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.Provider)
	}
}
