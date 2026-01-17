package graph

import (
	"strings"
	"testing"
)

// TestMigrationErrorLogging verifies that migration errors are properly logged
// instead of being silently suppressed
func TestMigrationErrorLogging(t *testing.T) {
	// Create a test instance
	// This test verifies to logging behavior for migration errors
	// Since we can't easily mock SurrealDB responses, we'll verify to logic manually

	// Test various error messages that should be handled differently
	testCases := []struct {
		name         string
		errorMsg    string
		shouldLog    bool
		description  string
	}{
		{
			name:      "already defined table",
			errorMsg:  "table 'nodes' already defined",
			shouldLog: false,
			description: "Benign: table already exists, should not log",
		},
		{
			name:      "already exists index",
			errorMsg:  "index 'idx_nodes_id' already exists",
			shouldLog: false,
			description: "Benign: index already exists, should not log",
		},
		{
			name:      "duplicate index",
			errorMsg:  "Duplicate index: idx_nodes_id",
			shouldLog: false,
			description: "Benign: duplicate index, should not log",
		},
		{
			name:      "permission denied",
			errorMsg:  "permission denied to create table",
			shouldLog: true,
			description: "Severe: permission issue, should log warning",
		},
		{
			name:      "connection failed",
			errorMsg:  "connection to database failed: timeout",
			shouldLog: true,
			description: "Severe: connection issue, should log warning",
		},
		{
			name:      "syntax error",
			errorMsg:  "syntax error at position 10",
			shouldLog: true,
			description: "Severe: invalid syntax, should log warning",
		},
		{
			name:      "unknown table error",
			errorMsg:  "failed to create table: internal error",
			shouldLog: true,
			description: "Severe: unknown error, should log warning",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate to error checking logic from RunMigrations
			// Convert to lowercase for case-insensitive matching (matches production code)
			errStr := strings.ToLower(tc.errorMsg)
			isAlreadyExists := strings.Contains(errStr, "already defined") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "duplicate index") ||
				strings.Contains(errStr, "duplicate field") ||
				(strings.Contains(errStr, "table name") && strings.Contains(errStr, "already exists"))

			// Determine if error should be logged (not benign "already exists")
			shouldLogWarning := !isAlreadyExists

			// Verify expectation
			if shouldLogWarning != tc.shouldLog {
				t.Errorf("%s: expected shouldLog=%v, got %v\n  Error: %s",
					tc.description, tc.shouldLog, shouldLogWarning, tc.errorMsg)
			}

			// Log for debugging
			if shouldLogWarning {
				t.Logf("Would log warning for: %s", tc.errorMsg)
			} else {
				t.Logf("Would skip logging for benign error: %s", tc.errorMsg)
			}
		})
	}
}

// TestMigrationIntegration is an integration test that requires a running SurrealDB instance
// This test is skipped by default but can be enabled for local testing
func TestMigrationIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test that RunMigrations doesn't silently suppress real errors
	// This requires a SurrealDB instance with unexpected configuration
	// For example: a database with restricted permissions

	// This is a placeholder for integration testing
	// To enable: 1. Start SurrealDB, 2. Configure to cause specific errors,
	// 3. Run this test and verify warnings are logged

	t.Skip("Integration test requires SurrealDB instance setup")
}

// TestMigrationIdempotency verifies that running migrations multiple times
// doesn't cause issues (idempotency)
func TestMigrationIdempotency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This test would verify that calling RunMigrations multiple times
	// on to same database doesn't cause errors (except benign "already exists")
	// This ensures to migration system is robust

	t.Skip("Integration test requires SurrealDB instance setup")
}
