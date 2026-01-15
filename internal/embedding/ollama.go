package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/heefoo/codeloom/internal/config"
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
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

func (p *OllamaProvider) Name() string {
	return "ollama"
}

func (p *OllamaProvider) Dimension() int {
	return p.dimension
}

func (p *OllamaProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
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
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		emb, err := p.EmbedSingle(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = emb
	}

	return embeddings, nil
}
