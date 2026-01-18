package util

import (
	"log"
	"path/filepath"
)

// MatchPattern wraps filepath.Match with proper error logging.
// Returns true if pattern matches name, false otherwise.
// Logs any pattern errors to help users fix malformed patterns.
//
// This is a shared utility used by both the indexer and watcher
// components to consistently handle file exclusion patterns.
func MatchPattern(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	if err != nil {
		log.Printf("Warning: invalid pattern '%s': %v. Pattern will not match any files.", pattern, err)
		return false
	}
	return matched
}
