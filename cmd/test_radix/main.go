package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	fmt.Println("Testing Clojure radix number validation fix...")

	// Initialize parser with symbol table disabled (we only care about syntax here)
	p := parser.NewParser()
	clojureLang := p.GetLanguage(parser.LangClojure)

	if clojureLang == nil {
		fmt.Println("ERROR: Clojure language not registered")
		return
	}

	clojureParser := sitter.NewParser()
	defer clojureParser.Close()
	clojureParser.SetLanguage(clojureLang)

	// Test cases
	testCases := []struct {
		name     string
		code     string
		shouldParse bool
		reason    string
	}{
		// Valid radix numbers that SHOULD parse
		{"Binary (base 2)", "2r1010", true, "Valid base 2 with digits 0-1"},
		{"Octal (base 8)", "8r17", true, "Valid base 8 with digits 0-7"},
		{"Decimal (base 10)", "10r256", true, "Valid base 10 with digits 0-9"},
		{"Hexadecimal (base 16)", "16rFF", true, "Valid base 16 with digits 0-9, A-F"},
		{"Base 36", "36rZ", true, "Valid base 36 with alphanumeric digits"},
		{"Base 3", "3r210", true, "Valid base 3 with digits 0-2"},
		{"Base 7", "7r16", true, "Valid base 7 with digits 0-6"},
		{"Base 11", "11rA", true, "Valid base 11 with digits 0-9, A"},
		{"Negative binary", "-2r101", true, "Valid negative radix number"},
		{"Base 36 complex", "36RBREATHESL0WLY", true, "Valid base 36 (from test corpus)"},

		// Invalid radix numbers that should NOT parse
		{"Invalid base 37", "37r10", false, "Base 37 is outside valid range (2-36)"},
		{"Invalid base 100", "100r5", false, "Base 100 is outside valid range (2-36)"},
		{"Invalid base 999", "999r1", false, "Base 999 is outside valid range (2-36)"},
		{"Single digit base 1", "1r0", false, "Base 1 is outside valid range (2-36)"},
		{"Zero base", "0r0", false, "Base 0 is outside valid range (2-36)"},
		{"Invalid base with letters", "ABr10", false, "Invalid base format (letters before r)"},
		{"No base", "r10", false, "Missing base number before r"},
	}

	passCount := 0
	failCount := 0

	for i, tc := range testCases {
		code := []byte(tc.code)
		ctx := context.Background()

		tree, err := clojureParser.ParseCtx(ctx, nil, code)
		if err != nil {
			fmt.Printf("Parse error for '%s': %v\n", tc.name, err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		hasError := rootNode.HasError()

		// Check if the code contains num_lit node
		containsNumLit := false
		for i := 0; i < int(rootNode.ChildCount()); i++ {
			child := rootNode.Child(i)
			if child != nil && child.Type() == "num_lit" {
				containsNumLit = true
				break
			}
		}

		passed := (containsNumLit && !hasError) == tc.shouldParse

		testNum := i + 1
		if passed {
			fmt.Printf("✓ Test %2d: %-25s %-20s OK\n", testNum, tc.name, fmt.Sprintf("\"%s\"", tc.code))
			passCount++
		} else {
			if tc.shouldParse {
				fmt.Printf("✗ Test %2d: %-25s %-20s FAILED - Expected to parse but didn't\n", testNum, tc.name, fmt.Sprintf("\"%s\"", tc.code))
				fmt.Printf("           Reason: %s\n", tc.reason)
			} else {
				fmt.Printf("✗ Test %2d: %-25s %-20s FAILED - Should have been rejected but parsed\n", testNum, tc.name, fmt.Sprintf("\"%s\"", tc.code))
				fmt.Printf("           Reason: %s\n", tc.reason)
			}
			failCount++
		}
	}

	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("Results: %d passed, %d failed out of %d tests\n", passCount, failCount, len(testCases))
	fmt.Println(strings.Repeat("=", 60))

	if failCount > 0 {
		fmt.Println("\nSome tests failed. Please review the grammar fix.")
	}
}
