package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	"github.com/heefoo/codeloom/internal/httpclient"
)

type OllamaProvider struct {
	baseURL   string
	model     string
	dimension int
	client    *http.Client
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func NewOllamaProvider(cfg config.EmbeddingConfig) (*OllamaProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	dimension := cfg.Dimension
	if dimension == 0 {
		dimension = 768 // Default for nomic-embed-text
	}

	return &OllamaProvider{
		baseURL:   baseURL,
		model:     cfg.Model,
		dimension: dimension,
		client:    httpclient.GetSharedClient(60 * time.Second),
	}, nil
}

func (p *OllamaProvider) Name() string {
	return "ollama"
}

func (p *OllamaProvider) Dimension() int {
	return p.dimension
}

func (p *OllamaProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("cannot embed empty text")
	}

	req := ollamaEmbedRequest{
		Model:  p.model,
		Prompt: text,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embedding error: %s - %s", resp.Status, string(body))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("ollama decode error: %w", err)
	}

	return embedResp.Embedding, nil
}

func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("cannot embed empty text list")
	}

	// Use concurrent requests for better performance
	// Ollama doesn't have a native batch API, so we parallelize requests
	const maxConcurrency = 10

	embeddings := make([][]float32, len(texts))
	errors := make([]error, len(texts))

	// Create semaphore for concurrency control
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i, text := range texts {
		wg.Add(1)
		go func(idx int, txt string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check context cancellation
			select {
			case <-ctx.Done():
				errors[idx] = ctx.Err()
				return
			default:
			}

			emb, err := p.EmbedSingle(ctx, txt)
			if err != nil {
				errors[idx] = err
				return
			}
			embeddings[idx] = emb
		}(i, text)
	}

	wg.Wait()

	// Check for errors - return partial results with error if any failed
	var firstError error
	errorCount := 0
	for i, err := range errors {
		if err != nil {
			if firstError == nil {
				firstError = fmt.Errorf("failed to embed text %d: %w", i, err)
			}
			errorCount++
		}
	}

	// Return partial results with error if any failed, but not if all failed
	if errorCount > 0 && errorCount < len(texts) {
		return embeddings, firstError
	} else if errorCount == len(texts) {
		return nil, firstError
	}

	return embeddings, nil
}
