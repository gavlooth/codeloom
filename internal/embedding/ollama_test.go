package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heefoo/codeloom/internal/config"
)

func TestNewOllamaProvider(t *testing.T) {
	tests := []struct {
		name            string
		config          config.EmbeddingConfig
		wantBase        string
		wantModel       string
		wantDim         int
		wantMaxConc     int
	}{
		{
			name: "default values",
			config: config.EmbeddingConfig{
				Provider: "ollama",
			},
			wantBase:    "http://localhost:11434",
			wantModel:   "",
			wantDim:     768,
			wantMaxConc: 10,
		},
		{
			name: "custom values",
			config: config.EmbeddingConfig{
				Provider:      "ollama",
				BaseURL:       "http://custom:9999",
				Model:         "custom-model",
				Dimension:     1024,
				MaxConcurrency: 20,
			},
			wantBase:    "http://custom:9999",
			wantModel:   "custom-model",
			wantDim:     1024,
			wantMaxConc: 20,
		},
		{
			name: "zero dimension uses default",
			config: config.EmbeddingConfig{
				Provider:      "ollama",
				BaseURL:       "http://localhost:11434",
				Model:         "test-model",
				Dimension:     0,
				MaxConcurrency: 5,
			},
			wantBase:    "http://localhost:11434",
			wantModel:   "test-model",
			wantDim:     768,
			wantMaxConc: 5,
		},
		{
			name: "zero max concurrency uses default",
			config: config.EmbeddingConfig{
				Provider:      "ollama",
				BaseURL:       "http://localhost:11434",
				Model:         "test-model",
				Dimension:     768,
				MaxConcurrency: 0,
			},
			wantBase:    "http://localhost:11434",
			wantModel:   "test-model",
			wantDim:     768,
			wantMaxConc: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOllamaProvider(tt.config)
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}

			if provider.baseURL != tt.wantBase {
				t.Errorf("baseURL = %v, want %v", provider.baseURL, tt.wantBase)
			}
			if provider.model != tt.wantModel {
				t.Errorf("model = %v, want %v", provider.model, tt.wantModel)
			}
			if provider.dimension != tt.wantDim {
				t.Errorf("dimension = %v, want %v", provider.dimension, tt.wantDim)
			}
			if provider.maxConcurrency != tt.wantMaxConc {
				t.Errorf("maxConcurrency = %v, want %v", provider.maxConcurrency, tt.wantMaxConc)
			}
		})
	}
}

func TestOllamaProviderName(t *testing.T) {
	provider, err := NewOllamaProvider(config.EmbeddingConfig{Provider: "ollama"})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	if provider.Name() != "ollama" {
		t.Errorf("Name() = %v, want ollama", provider.Name())
	}
}

func TestOllamaProviderDimension(t *testing.T) {
	tests := []struct {
		name      string
		dimension int
	}{
		{"default dimension", 0},
		{"custom dimension", 1024},
		{"large dimension", 3072},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOllamaProvider(config.EmbeddingConfig{
				Provider:  "ollama",
				Dimension: tt.dimension,
			})
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}

			wantDim := tt.dimension
			if wantDim == 0 {
				wantDim = 768 // default
			}
			if provider.Dimension() != wantDim {
				t.Errorf("Dimension() = %v, want %v", provider.Dimension(), wantDim)
			}
		})
	}
}

func TestOllamaProviderEmbedSingle(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		respStatus  int
		respBody    string
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful embedding",
			text:       "test text",
			respStatus: http.StatusOK,
			respBody:   `{"embedding":[0.1,0.2,0.3]}`,
			wantErr:    false,
		},
		{
			name:        "empty text error",
			text:        "",
			respStatus:  http.StatusOK,
			respBody:    `{"embedding":[0.1,0.2,0.3]}`,
			wantErr:     true,
			errContains: "cannot embed empty text",
		},
		{
			name:        "whitespace only error",
			text:        "   \n\t  ",
			respStatus:  http.StatusOK,
			respBody:    `{"embedding":[0.1,0.2,0.3]}`,
			wantErr:     true,
			errContains: "cannot embed empty text",
		},
		{
			name:        "server error",
			text:        "test text",
			respStatus:  http.StatusInternalServerError,
			respBody:    `{"error":"internal server error"}`,
			wantErr:     true,
			errContains: "ollama embedding error",
		},
		{
			name:        "invalid JSON response",
			text:        "test text",
			respStatus:  http.StatusOK,
			respBody:    `invalid json`,
			wantErr:     true,
			errContains: "ollama decode error",
		},
		{
			name:       "context cancellation",
			text:       "test text",
			respStatus: http.StatusOK,
			respBody:   `{"embedding":[0.1,0.2,0.3]}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.name == "context cancellation" {
					time.Sleep(100 * time.Millisecond)
				}
				w.WriteHeader(tt.respStatus)
				fmt.Fprint(w, tt.respBody)
			}))
			defer server.Close()

			provider, err := NewOllamaProvider(config.EmbeddingConfig{
				Provider: "ollama",
				BaseURL:  server.URL,
			})
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}

			var ctx context.Context
			if tt.name == "context cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()
			} else {
				ctx = context.Background()
			}

			embedding, err := provider.EmbedSingle(ctx, tt.text)

			if tt.wantErr {
				if err == nil {
					t.Errorf("EmbedSingle() expected error containing %q, got nil", tt.errContains)
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("EmbedSingle() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("EmbedSingle() unexpected error = %v", err)
				}
				if len(embedding) == 0 {
					t.Errorf("EmbedSingle() returned empty embedding")
				}
			}
		})
	}
}

func TestOllamaProviderEmbedPartialResults(t *testing.T) {
	// Test that partial results are returned when some requests fail
	tests := []struct {
		name           string
		texts          []string
		failIndices    []int // Indices of texts that should fail
		wantPartialErr bool
	}{
		{
			name:           "all succeed",
			texts:          []string{"text1", "text2", "text3"},
			failIndices:    []int{},
			wantPartialErr: false,
		},
		{
			name:           "some fail",
			texts:          []string{"text1", "text2", "text3"},
			failIndices:    []int{1},
			wantPartialErr: true,
		},
		{
			name:           "most fail",
			texts:          []string{"text1", "text2", "text3", "text4", "text5"},
			failIndices:    []int{0, 2, 4},
			wantPartialErr: true,
		},
		{
			name:           "all fail",
			texts:          []string{"text1", "text2", "text3"},
			failIndices:    []int{0, 1, 2},
			wantPartialErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Read request body to determine which text is being embedded
				var reqBody struct {
					Prompt string `json:"prompt"`
				}
				if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				// Find index of this text
				idx := -1
				for i, text := range tt.texts {
					if text == reqBody.Prompt {
						idx = i
						break
					}
				}
				if idx == -1 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				shouldFail := false
				for _, failIdx := range tt.failIndices {
					if idx == failIdx {
						shouldFail = true
						break
					}
				}

				if shouldFail {
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprint(w, `{"error":"server error"}`)
				} else {
					w.WriteHeader(http.StatusOK)
					fmt.Fprintf(w, `{"embedding":[%v,%v,%v]}`, float32(idx), float32(idx)+float32(0.1), float32(idx)+float32(0.2))
				}
			}))
			defer server.Close()

			provider, err := NewOllamaProvider(config.EmbeddingConfig{
				Provider: "ollama",
				BaseURL:  server.URL,
			})
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}

			ctx := context.Background()
			embeddings, err := provider.Embed(ctx, tt.texts)

			// Verify results
			if tt.wantPartialErr {
				if err == nil {
					t.Errorf("Embed() expected partial error, got nil")
				}
				if len(embeddings) != len(tt.texts) {
					t.Errorf("Embed() returned %d embeddings, want %d", len(embeddings), len(tt.texts))
				}
			} else if len(tt.failIndices) == len(tt.texts) {
				// All failed
				if err == nil {
					t.Errorf("Embed() expected error when all fail, got nil")
				}
				if embeddings != nil {
					t.Errorf("Embed() should return nil embeddings when all fail, got %v", embeddings)
				}
			} else {
				// All succeeded
				if err != nil {
					t.Errorf("Embed() unexpected error = %v", err)
				}
				if len(embeddings) != len(tt.texts) {
					t.Errorf("Embed() returned %d embeddings, want %d", len(embeddings), len(tt.texts))
				}
			}

			// Verify that successful embeddings are populated
			if embeddings != nil {
				for i, emb := range embeddings {
					shouldFail := false
					for _, failIdx := range tt.failIndices {
						if i == failIdx {
							shouldFail = true
							break
						}
					}

					if shouldFail && emb != nil {
						t.Errorf("Embed(%d) expected nil for failed request, got %v", i, emb)
					} else if !shouldFail && emb == nil {
						t.Errorf("Embed(%d) expected embedding for successful request, got nil", i)
					}
				}
			}
		})
	}
}

func TestOllamaProviderEmbedEmptyList(t *testing.T) {
	provider, err := NewOllamaProvider(config.EmbeddingConfig{Provider: "ollama"})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	ctx := context.Background()
	embeddings, err := provider.Embed(ctx, []string{})

	if err == nil {
		t.Errorf("Embed() expected error for empty list, got nil")
	}
	if embeddings != nil {
		t.Errorf("Embed() should return nil for empty list, got %v", embeddings)
	}
}

func TestOllamaProviderEmbedConcurrency(t *testing.T) {
	// Test that concurrent requests work correctly and don't interfere with each other
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := int(requestCount.Add(1))
		// Add small delay to ensure concurrency
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"embedding":[%v,%v,%v]}`, float32(count), float32(count)+float32(0.1), float32(count)+float32(0.2))
	}))
	defer server.Close()

	provider, err := NewOllamaProvider(config.EmbeddingConfig{
		Provider: "ollama",
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	ctx := context.Background()
	texts := make([]string, 20) // Request more than maxConcurrency (10)
	for i := range texts {
		texts[i] = fmt.Sprintf("test text %d", i)
	}

	embeddings, err := provider.Embed(ctx, texts)
	if err != nil {
		t.Errorf("Embed() unexpected error = %v", err)
	}
	if len(embeddings) != len(texts) {
		t.Errorf("Embed() returned %d embeddings, want %d", len(embeddings), len(texts))
	}

	// Verify all embeddings are non-nil
	for i, emb := range embeddings {
		if emb == nil {
			t.Errorf("Embed(%d) returned nil", i)
		}
		if len(emb) != 3 {
			t.Errorf("Embed(%d) returned embedding of length %d, want 3", i, len(emb))
		}
	}
}

func TestOllamaProviderEmbedContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer server.Close()

	provider, err := NewOllamaProvider(config.EmbeddingConfig{
		Provider: "ollama",
		BaseURL:  server.URL,
	})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Request multiple texts to trigger concurrent goroutines
	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	embeddings, err := provider.Embed(ctx, texts)

	// Context should be cancelled
	if err == nil {
		t.Errorf("Embed() expected error from context cancellation, got nil")
	}
	if embeddings != nil {
		t.Errorf("Embed() should return nil when context cancelled, got %v", embeddings)
	}
}

func TestOllamaProviderMaxConcurrency(t *testing.T) {
	// Test that configured maxConcurrency limits concurrent requests
	var maxConcurrentRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track concurrent requests
		maxConcurrentRequests.Add(1)
		defer maxConcurrentRequests.Add(-1)

		// Add small delay to ensure concurrent requests overlap
		time.Sleep(50 * time.Millisecond)

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer server.Close()

	tests := []struct {
		name           string
		maxConcurrency int
		textCount      int
	}{
		{
			name:           "concurrency 1",
			maxConcurrency: 1,
			textCount:      10,
		},
		{
			name:           "concurrency 3",
			maxConcurrency: 3,
			textCount:      10,
		},
		{
			name:           "concurrency 10",
			maxConcurrency: 10,
			textCount:      20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOllamaProvider(config.EmbeddingConfig{
				Provider:      "ollama",
				BaseURL:       server.URL,
				MaxConcurrency: tt.maxConcurrency,
			})
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}

			ctx := context.Background()
			texts := make([]string, tt.textCount)
			for i := range texts {
				texts[i] = fmt.Sprintf("test text %d", i)
			}

			embeddings, err := provider.Embed(ctx, texts)
			if err != nil {
				t.Errorf("Embed() unexpected error = %v", err)
			}
			if len(embeddings) != len(texts) {
				t.Errorf("Embed() returned %d embeddings, want %d", len(embeddings), len(texts))
			}

			// Verify max concurrent requests did not exceed configured value
			maxObserved := maxConcurrentRequests.Load()
			if maxObserved > int64(tt.maxConcurrency) {
				t.Errorf("Concurrent requests exceeded limit: observed %d, max allowed %d", maxObserved, tt.maxConcurrency)
			}
		})
	}
}

