package llm

import (
	"context"

	"github.com/heefoo/codeloom/internal/config"
)

type XAIProvider struct {
	*OpenAIProvider
}

func NewXAIProvider(cfg config.LLMConfig) (*XAIProvider, error) {
	// xAI uses OpenAI-compatible API
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.x.ai/v1"
	}

	openaiProvider, err := NewOpenAIProvider(cfg)
	if err != nil {
		return nil, err
	}

	return &XAIProvider{
		OpenAIProvider: openaiProvider,
	}, nil
}

func (p *XAIProvider) Name() string {
	return "xai"
}

func (p *XAIProvider) Generate(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	return p.OpenAIProvider.Generate(ctx, messages, opts...)
}

func (p *XAIProvider) GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error) {
	return p.OpenAIProvider.GenerateWithTools(ctx, messages, tools)
}

func (p *XAIProvider) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	return p.OpenAIProvider.Stream(ctx, messages, opts...)
}
