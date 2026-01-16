package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/heefoo/codeloom/internal/config"
)

type AnthropicProvider struct {
	client      *anthropic.Client
	model       string
	temperature float32
	maxTokens   int
}

func NewAnthropicProvider(cfg config.LLMConfig) (*AnthropicProvider, error) {
	opts := []option.RequestOption{}

	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := anthropic.NewClient(opts...)

	return &AnthropicProvider{
		client:      client,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
	}, nil
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) Generate(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Convert messages
	var systemPrompt string
	anthropicMessages := []anthropic.MessageParam{}

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemPrompt = m.Content
		case RoleUser:
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case RoleAssistant:
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	maxTokens := int64(options.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.F(p.model),
		MaxTokens: anthropic.F(maxTokens),
		Messages:  anthropic.F(anthropicMessages),
	}

	if systemPrompt != "" {
		params.System = anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(systemPrompt),
		})
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic completion error: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	// Extract text from content blocks
	var result string
	for _, block := range resp.Content {
		if block.Type == anthropic.ContentBlockTypeText {
			result += block.Text
		}
	}

	return result, nil
}

func (p *AnthropicProvider) GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error) {
	// Convert messages
	var systemPrompt string
	anthropicMessages := []anthropic.MessageParam{}

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemPrompt = m.Content
		case RoleUser:
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case RoleAssistant:
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	// Convert tools
	anthropicTools := make([]anthropic.ToolParam, len(tools))
	for i, t := range tools {
		anthropicTools[i] = anthropic.ToolParam{
			Name:        anthropic.F(t.Name),
			Description: anthropic.F(t.Description),
			InputSchema: anthropic.F(interface{}(t.Parameters)),
		}
	}

	maxTokens := int64(p.maxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.F(p.model),
		MaxTokens: anthropic.F(maxTokens),
		Messages:  anthropic.F(anthropicMessages),
		Tools:     anthropic.F(anthropicTools),
	}

	if systemPrompt != "" {
		params.System = anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(systemPrompt),
		})
	}

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic completion error: %w", err)
	}

	result := &ToolCallResponse{}

	for _, block := range resp.Content {
		switch block.Type {
		case anthropic.ContentBlockTypeText:
			result.Content += block.Text
		case anthropic.ContentBlockTypeToolUse:
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		}
	}

	return result, nil
}

func (p *AnthropicProvider) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Convert messages
	var systemPrompt string
	anthropicMessages := []anthropic.MessageParam{}

	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			systemPrompt = m.Content
		case RoleUser:
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case RoleAssistant:
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	maxTokens := int64(options.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 4096
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.F(p.model),
		MaxTokens: anthropic.F(maxTokens),
		Messages:  anthropic.F(anthropicMessages),
	}

	if systemPrompt != "" {
		params.System = anthropic.F([]anthropic.TextBlockParam{
			anthropic.NewTextBlock(systemPrompt),
		})
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan string)
	go func() {
		defer close(ch)

		for stream.Next() {
			event := stream.Current()
			switch delta := event.Delta.(type) {
			case anthropic.ContentBlockDeltaEventDelta:
				if delta.Type == "text_delta" {
					ch <- delta.Text
				}
			}
		}
	}()

	return ch, nil
}

func (p *AnthropicProvider) Close() error {
	return nil
}
