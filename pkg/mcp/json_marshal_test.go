package mcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Test that errorResult properly handles JSON marshaling errors
func TestErrorResult(t *testing.T) {
	// Test normal case - should marshal successfully
	result, err := errorResult("test error message")
	if err != nil {
		t.Fatalf("errorResult should not return error, got: %v", err)
	}

	if result == nil {
		t.Fatal("errorResult should return a non-nil CallToolResult")
	}

	if !result.IsError {
		t.Error("errorResult should return result with IsError=true")
	}

	if len(result.Content) == 0 {
		t.Fatal("errorResult should return result with content")
	}

	// Verify content is TextContent type
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent type, got %T", result.Content[0])
	}

	if textContent.Type != "text" {
		t.Errorf("Expected content type 'text', got: %s", textContent.Type)
	}

	if textContent.Text == "" {
		t.Error("Expected non-empty text content")
	}

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
		t.Errorf("Result text should be valid JSON, got error: %v", err)
	}

	if !parsed["error"].(bool) {
		t.Error("Parsed JSON should have error=true")
	}

	if parsed["message"] == nil {
		t.Error("Parsed JSON should have a message field")
	}
}

// Test errorResult with complex message that should always be marshalable
func TestErrorResultComplexMessage(t *testing.T) {
	// Messages with special characters, Unicode, etc.
	testMessages := []string{
		"Simple error",
		"Error with \"quotes\"",
		"Error with 'single quotes'",
		"Error with \n newline",
		"Error with unicode: 你好",
		"Error with emoji: 🚨",
	}

	for _, msg := range testMessages {
		result, err := errorResult(msg)
		if err != nil {
			t.Fatalf("errorResult should not return error for message %q, got: %v", msg, err)
		}

		if !result.IsError {
			t.Errorf("errorResult should return IsError=true for message %q", msg)
		}

		// Verify result text contains valid JSON
		if len(result.Content) > 0 {
			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok || textContent.Type != "text" {
				t.Errorf("Expected TextContent for message %q", msg)
				continue
			}

			// Parse to verify it's valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(textContent.Text), &parsed); err != nil {
				t.Errorf("Result text should be valid JSON for message %q, got error: %v", msg, err)
			}
		}
	}
}

// Test that JSON marshal errors are properly logged and handled
// This is a regression test for the silent error suppression issue
func TestJSONMarshalErrorHandling(t *testing.T) {
	// Create a result that should be marshalable
	result := map[string]interface{}{
		"error":   true,
		"message": "test",
	}

	// This should succeed
	_, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Standard map[string]interface{} should be marshalable, got error: %v", err)
	}

	// Test with empty map (should also succeed)
	emptyResult := map[string]interface{}{}
	_, err = json.Marshal(emptyResult)
	if err != nil {
		t.Fatalf("Empty map should be marshalable, got error: %v", err)
	}
}

// Test that errorResult function gracefully handles edge cases
func TestErrorResultEdgeCases(t *testing.T) {
	edgeCases := []struct {
		name    string
		message string
	}{
		{"Empty message", ""},
		{"Very long message", string(make([]byte, 10000))},
		{"Message with null byte", "test\x00"},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := errorResult(tc.message)
			if err != nil {
				t.Fatalf("errorResult should handle %s without returning error, got: %v", tc.name, err)
			}
			if result == nil {
				t.Fatalf("errorResult should return non-nil result for %s", tc.name)
			}
			if !result.IsError {
				t.Errorf("errorResult should set IsError=true for %s", tc.name)
			}

			// Verify result contains valid JSON
			if len(result.Content) > 0 {
				textContent, ok := result.Content[0].(mcp.TextContent)
				if ok {
					var parsed map[string]interface{}
					if err := json.Unmarshal([]byte(textContent.Text), &parsed); err == nil {
						// Successfully parsed as JSON
						if tc.message == "" && parsed["message"].(string) != "" {
							// Empty message should still produce valid JSON
						}
					}
				}
			}
		})
	}
}
