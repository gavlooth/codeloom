package config

import (
	"os"
	"testing"
)

// TestDefaultConfig verifies default configuration values
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test server defaults
	if cfg.Server.Mode != "stdio" {
		t.Errorf("Expected default Mode 'stdio', got '%s'", cfg.Server.Mode)
	}
	if cfg.Server.Port != 3003 {
		t.Errorf("Expected default Port 3003, got %d", cfg.Server.Port)
	}
	if cfg.Server.WatcherDebounceMs != 100 {
		t.Errorf("Expected default WatcherDebounceMs 100, got %d", cfg.Server.WatcherDebounceMs)
	}

	t.Log("PASS: Default config values are correct")
}

// TestConfigValidation verifies configuration validation logic
func TestConfigValidation(t *testing.T) {
	// Test valid config
	cfg := DefaultConfig()
	warnings := Validate(cfg)
	if len(warnings) > 0 {
		t.Errorf("Expected no validation warnings for default config, got %d warnings", len(warnings))
		for _, w := range warnings {
			t.Logf("Warning: %s", w)
		}
	}

	// Test invalid watcher debounce (too low)
	cfg.Server.WatcherDebounceMs = 5
	warnings = Validate(cfg)
	found := false
	for _, w := range warnings {
		if contains(w, "debounce") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected validation warning for watcher debounce < 10ms")
	}

	// Test invalid watcher debounce (too high)
	cfg.Server.WatcherDebounceMs = 70000
	warnings = Validate(cfg)
	found = false
	for _, w := range warnings {
		if contains(w, "debounce") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected validation warning for watcher debounce > 60000ms")
	}

	t.Log("PASS: Config validation works correctly")
}

// TestEnvOverrideWatcherDebounce verifies environment variable override
func TestEnvOverrideWatcherDebounce(t *testing.T) {
	// Save original env value
	origVal := os.Getenv("CODELOOM_WATCHER_DEBOUNCE_MS")
	defer func() {
		if origVal == "" {
			os.Unsetenv("CODELOOM_WATCHER_DEBOUNCE_MS")
		} else {
			os.Setenv("CODELOOM_WATCHER_DEBOUNCE_MS", origVal)
		}
	}()

	// Test with custom debounce value
	os.Setenv("CODELOOM_WATCHER_DEBOUNCE_MS", "500")

	cfg := DefaultConfig()
	applyEnvOverrides(cfg)

	if cfg.Server.WatcherDebounceMs != 500 {
		t.Errorf("Expected WatcherDebounceMs 500 from env, got %d", cfg.Server.WatcherDebounceMs)
	}

	t.Log("PASS: Environment variable override works for watcher debounce")
}

func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
