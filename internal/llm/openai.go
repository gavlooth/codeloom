package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	openai "github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
	client      *openai.Client
	model       string
	temperature float32
	maxTokens   int
	name        string
}

func NewOpenAIProvider(cfg config.LLMConfig) (*OpenAIProvider, error) {
	clientCfg := openai.DefaultConfig(cfg.APIKey)

	// Set custom base URL for OpenAI-compatible providers
	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}

	// Set timeout
	if cfg.TimeoutSecs > 0 {
		clientCfg.HTTPClient = &http.Client{
			Timeout: time.Duration(cfg.TimeoutSecs) * time.Second,
		}
	}

	client := openai.NewClientWithConfig(clientCfg)

	name := "openai"
	if cfg.Provider == "openai-compatible" {
		name = "openai-compatible"
	}

	return &OpenAIProvider{
		client:      client,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		name:        name,
	}, nil
}

func (p *OpenAIProvider) Name() string {
	return p.name
}

func (p *OpenAIProvider) Generate(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, m := range messages {
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
		if m.ToolCallID != "" {
			openaiMessages[i].ToolCallID = m.ToolCallID
		}
	}

	req := openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    openaiMessages,
		Temperature: options.Temperature,
		MaxTokens:   options.MaxTokens,
	}

	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai completion error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}

	return resp.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error) {
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, m := range messages {
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
		if m.ToolCallID != "" {
			openaiMessages[i].ToolCallID = m.ToolCallID
		}
	}

	openaiTools := make([]openai.Tool, len(tools))
	for i, t := range tools {
		paramsJSON, err := json.Marshal(t.Parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool parameters for %s: %w", t.Name, err)
		}
		openaiTools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(paramsJSON),
			},
		}
	}

	req := openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    openaiMessages,
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
		Tools:       openaiTools,
	}

	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai completion error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned")
	}

	choice := resp.Choices[0]
	result := &ToolCallResponse{
		Content: choice.Message.Content,
	}

	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return result, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, m := range messages {
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
	}

	req := openai.ChatCompletionRequest{
		Model:       p.model,
		Messages:    openaiMessages,
		Temperature: options.Temperature,
		MaxTokens:   options.MaxTokens,
		Stream:      true,
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("openai stream error: %w", err)
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			if len(resp.Choices) > 0 {
				ch <- resp.Choices[0].Delta.Content
			}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) Close() error {
	return nil
}
