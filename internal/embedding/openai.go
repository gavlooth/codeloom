package embedding

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/heefoo/codeloom/internal/config"
	openai "github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
	client    *openai.Client
	model     string
	dimension int
}

func NewOpenAIProvider(cfg config.EmbeddingConfig) (*OpenAIProvider, error) {
	clientCfg := openai.DefaultConfig(cfg.APIKey)

	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}

	clientCfg.HTTPClient = &http.Client{
		Timeout: 60 * time.Second,
	}

	client := openai.NewClientWithConfig(clientCfg)

	dimension := cfg.Dimension
	if dimension == 0 {
		dimension = 1536 // Default for text-embedding-3-small
	}

	model := cfg.Model
	if model == "" {
		model = string(openai.SmallEmbedding3)
	}

	return &OpenAIProvider{
		client:    client,
		model:     model,
		dimension: dimension,
	}, nil
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) Dimension() int {
	return p.dimension
}

func (p *OpenAIProvider) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(p.model),
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedding error: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return resp.Data[0].Embedding, nil
}

func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: openai.EmbeddingModel(p.model),
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedding error: %w", err)
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}
