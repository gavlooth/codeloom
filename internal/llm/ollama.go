package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/httpclient"
)

type OllamaProvider struct {
	baseURL     string
	model       string
	temperature float32
	maxTokens   int
	client      *http.Client
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaOptions struct {
	Temperature float32 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ollamaChatResponse struct {
	Model      string        `json:"model"`
	Message    ollamaMessage `json:"message"`
	Done       bool          `json:"done"`
	DoneReason string        `json:"done_reason,omitempty"`
}

func NewOllamaProvider(cfg config.LLMConfig) (*OllamaProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second // Default 2 minute timeout
	}

	return &OllamaProvider{
		baseURL:     baseURL,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		client:      httpclient.GetSharedClient(timeout),
	}, nil
}

func (p *OllamaProvider) Name() string {
	return "ollama"
}

func (p *OllamaProvider) Generate(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	req := ollamaChatRequest{
		Model:    p.model,
		Messages: ollamaMessages,
		Stream:   false,
		Options: ollamaOptions{
			Temperature: options.Temperature,
			NumPredict:  options.MaxTokens,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("ollama error: %s - failed to read response body: %v", resp.Status, err)
		}
		return "", fmt.Errorf("ollama error: %s - %s", resp.Status, string(body))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("ollama decode error: %w", err)
	}

	return chatResp.Message.Content, nil
}

func (p *OllamaProvider) GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error) {
	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	ollamaTools := make([]ollamaTool, len(tools))
	for i, t := range tools {
		ollamaTools[i] = ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}

	req := ollamaChatRequest{
		Model:    p.model,
		Messages: ollamaMessages,
		Stream:   false,
		Tools:    ollamaTools,
		Options: ollamaOptions{
			Temperature: p.temperature,
			NumPredict:  p.maxTokens,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("ollama error: %s - failed to read response body: %v", resp.Status, err)
		}
		return nil, fmt.Errorf("ollama error: %s - %s", resp.Status, string(body))
	}

	var chatResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ollama decode error: %w", err)
	}

	result := &ToolCallResponse{
		Content: chatResp.Message.Content,
	}

	for i, tc := range chatResp.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      tc.Function.Name,
			Arguments: string(tc.Function.Arguments),
		})
	}

	return result, nil
}

func (p *OllamaProvider) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	ollamaMessages := make([]ollamaMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
	}

	req := ollamaChatRequest{
		Model:    p.model,
		Messages: ollamaMessages,
		Stream:   true,
		Options: ollamaOptions{
			Temperature: options.Temperature,
			NumPredict:  options.MaxTokens,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("ollama error: %s - failed to read response body: %v", resp.Status, err)
		}
		return nil, fmt.Errorf("ollama error: %s - %s", resp.Status, string(body))
	}

	// Create channel after all error checks to avoid leak on error paths
	ch := make(chan string, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for {
			// Check for context cancellation at loop start
			select {
			case <-ctx.Done():
				return
			default:
			}

			if !scanner.Scan() {
				// Check for scanner errors
				if err := scanner.Err(); err != nil {
					log.Printf("ollama stream error: scanner error: %v", err)
				}
				return
			}

			var chatResp ollamaChatResponse
			if err := json.Unmarshal(scanner.Bytes(), &chatResp); err != nil {
				log.Printf("ollama stream error: failed to unmarshal JSON: %v", err)
				continue
			}
			if chatResp.Message.Content != "" {
				// Non-blocking send with context check
				select {
				case ch <- chatResp.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if chatResp.Done {
				return
			}
		}
	}()

	return ch, nil
}

func (p *OllamaProvider) Close() error {
	return nil
}
