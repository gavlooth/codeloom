package main

import (
	"context"
	"fmt"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	fmt.Println("Debugging Clojure radix number parsing...")

	p := parser.NewParser()
	clojureLang := p.GetLanguage(parser.LangClojure)
	clojureParser := sitter.NewParser()
	defer clojureParser.Close()
	clojureParser.SetLanguage(clojureLang)

	// Test invalid radix numbers
	testCases := []struct {
		name string
		code string
	}{
		{"Invalid base 37", "37r10"},
		{"Invalid base 100", "100r5"},
		{"Invalid base 999", "999r1"},
		{"Single digit base 1", "1r0"},
		{"Zero base", "0r0"},
	}

	for _, tc := range testCases {
		fmt.Printf("\n=== Testing: %s (%s) ===\n", tc.name, tc.code)
		ctx := context.Background()
		tree, err := clojureParser.ParseCtx(ctx, nil, []byte(tc.code))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		fmt.Printf("Root node type: %s\n", rootNode.Type())
		fmt.Printf("Has error: %v\n", rootNode.HasError())
		fmt.Printf("Parse tree:\n%s\n", rootNode.String())

		// Check children
		for j := 0; j < int(rootNode.ChildCount()); j++ {
			child := rootNode.Child(j)
			if child != nil {
				fmt.Printf("Child %d: type=%s, text=%q\n", j, child.Type(), string(tc.code[child.StartByte():child.EndByte()]))
			}
		}
	}
}
