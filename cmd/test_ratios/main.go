package main

import (
	"fmt"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
	commonlisp_lang "github.com/heefoo/codeloom/internal/parser/grammars/commonlisp_lang"
)

// Test if a ratio string parses as a number literal (num_lit)
// Invalid ratios with zero denominator will parse as symbol (sym_lit) instead
func testRatio(ratio string) bool {
	code := "(defun test-func () " + ratio + ")"
	
	parser := sitter.NewParser()
	defer parser.Close()
	
	parser.SetLanguage(commonlisp_lang.GetLanguage())
	
	content := []byte(code)
	tree := parser.Parse(nil, content)
	
	if tree == nil {
		return false
	}
	defer tree.Close()
	
	root := tree.RootNode()
	treeStr := root.String()
	
	// Check tree string for "(num_lit)" vs "(sym_lit)" at the end
	// Valid ratios produce: ... value: (num_lit))))
	// Invalid ratios produce: ... value: (sym_lit))))
	isNumLit := contains(treeStr, "value: (num_lit))")
	
	return isNumLit
}

// Simple string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func main() {
	tests := []struct{
		code   string
		expect bool
	}{
		{"1/2", true},   // Valid ratio
		{"0/5", true},   // Valid ratio (0/5 is 0)
		{"10/20", true}, // Valid ratio
		{"1/0", false},  // Invalid (division by zero)
		{"0/0", false},  // Invalid (division by zero)
		{"5/0", false},  // Invalid (division by zero)
	}

	passed := 0
	failed := 0

	for _, t := range tests {
		result := testRatio(t.code)
		status := "✓ PASS"
		if result != t.expect {
			status = "✗ FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Printf("%s: code=%s, expect=%v, got=%v\n", status, t.code, t.expect, result)
	}

	fmt.Printf("\nResults: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
