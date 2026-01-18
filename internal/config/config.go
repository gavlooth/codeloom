package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LLM       LLMConfig       `toml:"llm"`
	Embedding EmbeddingConfig `toml:"embedding"`
	Database  DatabaseConfig  `toml:"database"`
	Server    ServerConfig    `toml:"server"`
}

type LLMConfig struct {
	Enabled       bool    `toml:"enabled"`
	Provider      string  `toml:"provider"`
	Model         string  `toml:"model"`
	APIKey        string  `toml:"api_key"`
	BaseURL       string  `toml:"base_url"`
	Temperature   float32 `toml:"temperature"`
	MaxTokens     int     `toml:"max_tokens"`
	ContextWindow int     `toml:"context_window"`
	TimeoutSecs   int     `toml:"timeout_secs"`
}

type EmbeddingConfig struct {
	Provider  string `toml:"provider"`
	Model     string `toml:"model"`
	Dimension int    `toml:"dimension"`
	BaseURL   string `toml:"base_url"`
	APIKey    string `toml:"api_key"`
	BatchSize int    `toml:"batch_size"`
}

type DatabaseConfig struct {
	Backend   string          `toml:"backend"`
	SurrealDB SurrealDBConfig `toml:"surrealdb"`
}

type SurrealDBConfig struct {
	URL       string `toml:"url"`
	Namespace string `toml:"namespace"`
	Database  string `toml:"database"`
	Username  string `toml:"username"`
	Password  string `toml:"password"`
}

type ServerConfig struct {
	Mode              string `toml:"mode"`
	Port              int    `toml:"port"`
	WatcherDebounceMs int    `toml:"watcher_debounce_ms"`
	IndexTimeoutMs    int    `toml:"index_timeout_ms"`
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	// Try to load from file
	if path != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
	} else {
		// Try default locations
		locations := []string{
			".codeloom/config.toml",
			filepath.Join(os.Getenv("HOME"), ".codeloom/config.toml"),
			"/etc/codeloom/config.toml",
		}
		for _, loc := range locations {
			if _, err := os.Stat(loc); err == nil {
				if _, err := toml.DecodeFile(loc, cfg); err == nil {
					break
				}
			}
		}
	}

	// Override with environment variables
	applyEnvOverrides(cfg)

	return cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Enabled:       true,
			Provider:      "openai-compatible",
			Model:         "gpt-4",
			Temperature:   0.1,
			MaxTokens:     4096,
			ContextWindow: 128000,
			TimeoutSecs:   120,
		},
		Embedding: EmbeddingConfig{
			Provider:  "ollama",
			Model:     "nomic-embed-text",
			Dimension: 768,
			BaseURL:   "http://localhost:11434",
			BatchSize: 64,
		},
		Database: DatabaseConfig{
			Backend: "surrealdb",
			SurrealDB: SurrealDBConfig{
				URL:       "ws://localhost:3004",
				Namespace: "codeloom",
				Database:  "main",
				Username:  "root",
				Password:  "root",
			},
		},
		Server: ServerConfig{
			Mode:              "stdio",
			Port:              3003,
			WatcherDebounceMs: 100,
			IndexTimeoutMs:    60000, // Default 60 second timeout for indexing operations
		},
	}
}

func Validate(cfg *Config) []string {
	var warnings []string

	// Validate LLM settings
	if cfg.LLM.Enabled {
		if cfg.LLM.Provider == "" {
			warnings = append(warnings, "LLM provider is enabled but no provider specified")
		}
		if cfg.LLM.MaxTokens < 1 {
			warnings = append(warnings, "LLM MaxTokens must be at least 1")
		}
		if cfg.LLM.MaxTokens > 128000 {
			warnings = append(warnings, "LLM MaxTokens exceeds reasonable maximum (128000)")
		}
		if cfg.LLM.Temperature < 0 || cfg.LLM.Temperature > 2 {
			warnings = append(warnings, "LLM Temperature must be between 0 and 2")
		}
		if cfg.LLM.TimeoutSecs < 1 {
			warnings = append(warnings, "LLM TimeoutSecs must be at least 1 second")
		}
		if cfg.LLM.TimeoutSecs > 600 {
			warnings = append(warnings, "LLM TimeoutSecs exceeds reasonable maximum (600 seconds)")
		}
	}

	// Validate embedding settings
	if cfg.Embedding.Provider == "" {
		warnings = append(warnings, "Embedding provider is empty")
	}
	if cfg.Embedding.Dimension < 1 || cfg.Embedding.Dimension > 10000 {
		warnings = append(warnings, "Embedding dimension must be between 1 and 10000")
	}
	if cfg.Embedding.BatchSize < 1 || cfg.Embedding.BatchSize > 1000 {
		warnings = append(warnings, "Embedding batch size must be between 1 and 1000")
	}

	// Validate database settings
	if cfg.Database.Backend == "surrealdb" {
		if cfg.Database.SurrealDB.URL == "" {
			warnings = append(warnings, "SurrealDB URL cannot be empty")
		}
		if cfg.Database.SurrealDB.Namespace == "" {
			warnings = append(warnings, "SurrealDB namespace cannot be empty")
		}
		if cfg.Database.SurrealDB.Database == "" {
			warnings = append(warnings, "SurrealDB database cannot be empty")
		}
	}

	// Validate server settings
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		warnings = append(warnings, "Server port must be between 1 and 65535")
	}
	if cfg.Server.WatcherDebounceMs < 10 {
		warnings = append(warnings, "Watcher debounce must be at least 10ms")
	}
	if cfg.Server.WatcherDebounceMs > 60000 {
		warnings = append(warnings, "Watcher debounce exceeds reasonable maximum (60000ms)")
	}
	if cfg.Server.IndexTimeoutMs < 1000 {
		warnings = append(warnings, "Index timeout must be at least 1 second")
	}
	if cfg.Server.IndexTimeoutMs > 300000 {
		warnings = append(warnings, "Index timeout exceeds reasonable maximum (300 seconds)")
	}

	return warnings
}

func applyEnvOverrides(cfg *Config) {
	// LLM settings
	if v := os.Getenv("CODELOOM_LLM_PROVIDER"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := os.Getenv("CODELOOM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
	if v := os.Getenv("CODELOOM_OPENAI_COMPATIBLE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" && cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" && cfg.LLM.Provider == "anthropic" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("CODELOOM_CONTEXT_WINDOW"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.LLM.ContextWindow = i
		}
	}
	if v := os.Getenv("CODELOOM_MAX_TOKENS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.LLM.MaxTokens = i
		}
	}

	// Embedding settings
	if v := os.Getenv("CODELOOM_EMBEDDING_PROVIDER"); v != "" {
		cfg.Embedding.Provider = v
	}
	if v := os.Getenv("CODELOOM_EMBEDDING_MODEL"); v != "" {
		cfg.Embedding.Model = v
	}
	if v := os.Getenv("CODELOOM_EMBEDDING_DIMENSION"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Embedding.Dimension = i
		}
	}
	if v := os.Getenv("CODELOOM_OLLAMA_URL"); v != "" {
		cfg.Embedding.BaseURL = v
	}

	// Database settings
	if v := os.Getenv("CODELOOM_SURREALDB_URL"); v != "" {
		cfg.Database.SurrealDB.URL = v
	}
	if v := os.Getenv("CODELOOM__DATABASE__SURREALDB__CONNECTION"); v != "" {
		cfg.Database.SurrealDB.URL = v
	}
	if v := os.Getenv("CODELOOM_SURREALDB_NAMESPACE"); v != "" {
		cfg.Database.SurrealDB.Namespace = v
	}
	if v := os.Getenv("CODELOOM__DATABASE__SURREALDB__NAMESPACE"); v != "" {
		cfg.Database.SurrealDB.Namespace = v
	}
	if v := os.Getenv("CODELOOM_SURREALDB_DATABASE"); v != "" {
		cfg.Database.SurrealDB.Database = v
	}
	if v := os.Getenv("CODELOOM__DATABASE__SURREALDB__DATABASE"); v != "" {
		cfg.Database.SurrealDB.Database = v
	}
	if v := os.Getenv("CODELOOM_SURREALDB_USERNAME"); v != "" {
		cfg.Database.SurrealDB.Username = v
	}
	if v := os.Getenv("CODELOOM_SURREALDB_PASSWORD"); v != "" {
		cfg.Database.SurrealDB.Password = v
	}

	// Server settings
	if v := os.Getenv("CODELOOM_WATCHER_DEBOUNCE_MS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Server.WatcherDebounceMs = i
		}
	}
	if v := os.Getenv("CODELOOM_INDEX_TIMEOUT_MS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.Server.IndexTimeoutMs = i
		}
	}
}
