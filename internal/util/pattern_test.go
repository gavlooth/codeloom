package util

import (
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		filename string
		want     bool
	}{
		{
			name:     "exact match",
			pattern:  "node_modules",
			filename: "node_modules",
			want:     true,
		},
		{
			name:     "wildcard match",
			pattern:  "*.min.js",
			filename: "app.min.js",
			want:     true,
		},
		{
			name:     "wildcard no match",
			pattern:  "*.min.js",
			filename: "app.js",
			want:     false,
		},
		{
			name:     "question mark",
			pattern:  "test?.go",
			filename: "test1.go",
			want:     true,
		},
		{
			name:     "question mark no match",
			pattern:  "test?.go",
			filename: "test12.go",
			want:     false,
		},
		{
			name:     "character class",
			pattern:  "test[123].go",
			filename: "test2.go",
			want:     true,
		},
		{
			name:     "negated character class",
			pattern:  "test[!123].go",
			filename: "test4.go",
			want:     false, // filepath.Match doesn't support negated character classes
		},
		{
			name:     "dot files",
			pattern:  ".git",
			filename: ".git",
			want:     true,
		},
		{
			name:     "double star not supported by filepath.Match",
			pattern:  "**/*.go",
			filename: "test.go",
			want:     false, // filepath.Match doesn't support **
		},
		{
			name:     "directory name match",
			pattern:  "build",
			filename: "build",
			want:     true,
		},
		{
			name:     "empty pattern",
			pattern:  "",
			filename: "test",
			want:     false,
		},
		{
			name:     "empty filename",
			pattern:  "*.go",
			filename: "",
			want:     false, // filepath.Match returns false for empty strings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchPattern(tt.pattern, tt.filename)
			if got != tt.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.filename, got, tt.want)
			}
		})
	}
}

func TestMatchPatternInvalid(t *testing.T) {
	// Test that invalid patterns are handled gracefully (return false, log warning)
	// These should not panic, just return false
	invalidPatterns := []string{
		"[",           // unclosed bracket
		"[*",          // invalid bracket pattern
		"???",         // valid but tests edge case
		"[a-]",        // invalid range
	}

	for _, pattern := range invalidPatterns {
		t.Run(pattern, func(t *testing.T) {
			// Should not panic and should return false
			got := MatchPattern(pattern, "test")
			if got {
				t.Errorf("MatchPattern(%q, %q) = %v, want false (invalid pattern)", pattern, "test", got)
			}
		})
	}
}

func TestMatchPatternCommonExclusions(t *testing.T) {
	// Test patterns commonly used in code exclusion
	tests := []struct {
		pattern  string
		filename string
		want     bool
	}{
		{".git", ".git", true},
		{"node_modules", "node_modules", true},
		{"vendor", "vendor", true},
		{"__pycache__", "__pycache__", true},
		{".venv", ".venv", true},
		{"*.min.js", "bundle.min.js", true},
		{"*.min.js", "bundle.js", false},
		{"*.map", "bundle.map", true},
		{".idea", ".idea", true},
		{".vscode", ".vscode", true},
		{"target", "target", true},
		{"build", "build", true},
		{"dist", "dist", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.filename, func(t *testing.T) {
			got := MatchPattern(tt.pattern, tt.filename)
			if got != tt.want {
				t.Errorf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.filename, got, tt.want)
			}
		})
	}
}
