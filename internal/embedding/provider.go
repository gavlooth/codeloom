package embedding

import (
	"context"
	"fmt"

	"github.com/heefoo/codeloom/internal/config"
)

type Provider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	EmbedSingle(ctx context.Context, text string) ([]float32, error)
	Dimension() int
	Name() string
}

func NewProvider(cfg config.EmbeddingConfig) (Provider, error) {
	switch cfg.Provider {
	case "ollama":
		return NewOllamaProvider(cfg)
	case "openai":
		return NewOpenAIProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Provider)
	}
}
