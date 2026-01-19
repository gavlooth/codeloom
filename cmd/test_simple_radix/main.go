package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	fmt.Println("Testing specific radix patterns...")

	p := parser.NewParser()
	clojureLang := p.GetLanguage(parser.LangClojure)
	clojureParser := sitter.NewParser()
	defer clojureParser.Close()
	clojureParser.SetLanguage(clojureLang)

	testCases := []string{
		"2r1010",     // Valid base 2
		"8r17",       // Valid base 8
		"10r256",      // Valid base 10
		"16rFF",       // Valid base 16
		"36rZ",        // Valid base 36
		"3r210",       // Valid base 3 (generic)
		"7r16",        // Valid base 7 (generic)
		"11rA",        // Valid base 11 (generic)
		"20rJ",        // Valid base 20 (generic)
		"30rN",        // Valid base 30 (generic)
		"35rZ",        // Valid base 35 (generic)
		"37r10",       // Invalid: base 37 > 36
		"40r10",       // Invalid: base 40 > 36
		"99r10",       // Invalid: base 99 > 36
		"0r0",         // Invalid: base 0 < 2
		"1r0",         // Invalid: base 1 < 2
	}

	fmt.Println(strings.Repeat("=", 80))
	for i, code := range testCases {
		ctx := context.Background()
		tree, err := clojureParser.ParseCtx(ctx, nil, []byte(code))
		if err != nil {
			fmt.Printf("%-15s %-10s PARSE ERROR: %v\n", code, "", err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		hasError := rootNode.HasError()
		status := ""

		// For invalid radix numbers, they may parse as INTEGER + SYMBOL (e.g., "37" + "r10")
		// We need to check if it parsed as a SINGLE num_lit node (correct)
		// or as multiple nodes (which means base was rejected)

		singleNumLit := rootNode.ChildCount() == 1 && rootNode.Child(0).Type() == "num_lit"
		if singleNumLit {
			// Check if the full text matches the input (single node covering entire input)
			child := rootNode.Child(0)
			if int(child.EndByte()-child.StartByte()) == len(code) {
				status = "NUM_LIT"
			} else {
				// num_lit doesn't cover entire input - parsed incorrectly
				status = "PARTIAL"
			}
		} else if rootNode.ChildCount() > 1 {
			// Multiple children - likely INTEGER + SYMBOL
			status = "SPLIT"
		} else {
			status = "SYMBOL"
		}

		if hasError {
			status = "ERROR"
		}

		testNum := i + 1
		if testNum <= 11 {
			fmt.Printf("Test %2d: %-15s %-10s (expected: VALID)\n", testNum, code, status)
		} else {
			fmt.Printf("Test %2d: %-15s %-10s (expected: INVALID)\n", testNum, code, status)
		}
	}
	fmt.Println(strings.Repeat("=", 80))
}
