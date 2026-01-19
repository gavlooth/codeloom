package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	fmt.Println("Testing flet/labels/macrolet parameter handling...")

	// Test cases with flet/labels/macrolet examples
	testCases := []struct {
		name string
		code string
	}{
		{
			name: "Simple flet with two parameters",
			code: "(flet ((func-1 (param1 param2) (+ param1 param2))) (func-1 3 4))",
		},
		{
			name: "flet with multiple parameters",
			code: "(flet ((func-1 (param1 param2) (+ param1 param2))) (func-1 3 4))",
		},
		{
			name: "labels with recursive function",
			code: "(labels ((factorial (n) (if (<= n 1) 1 (* n (factorial (- n 1)))))) (factorial 5))",
		},
		{
			name: "macrolet with parameter",
			code: "(macrolet ((inc (x) `(1+ ,x))) (inc 5))",
		},
		{
			name: "let with bindings",
			code: "(let ((x 10) (y 20)) (+ x y))",
		},
		{
			name: "let* with sequential bindings",
			code: "(let* ((x 10) (y (+ x 5))) (+ x y))",
		},
		{
			name: "Nested flet calls (should not be tagged as function calls)",
			code: "(flet ((square (x) (* x x))) (square (square 3)))",
		},
	}

	p := parser.NewParser()
	clLang := p.GetLanguage(parser.LangCommonLisp)
	clParser := sitter.NewParser()
	defer clParser.Close()
	clParser.SetLanguage(clLang)

	// Read the actual tags.scm file
	tagsSCMPath := "/home/heefoo/codeloom/internal/parser/grammars/commonlisp/queries/tags.scm"
	queryContent, err := os.ReadFile(tagsSCMPath)
	if err != nil {
		fmt.Printf("Error reading %s: %v\n", tagsSCMPath, err)
		return
	}
	query, err := sitter.NewQuery(queryContent, clLang)
	if err != nil {
		fmt.Printf("Error creating query from %s: %v\n", tagsSCMPath, err)
		return
	}
	defer query.Close()

	for _, tc := range testCases {
		fmt.Printf("\n=== %s ===\n", tc.name)
		fmt.Printf("Code: %s\n\n", strings.ReplaceAll(tc.code, "\n", " "))

		ctx := context.Background()
		tree, err := clParser.ParseCtx(ctx, nil, []byte(tc.code))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		if rootNode.HasError() {
			fmt.Printf("Parse tree has errors\n")
		}

		// Execute query
		qc := sitter.NewQueryCursor()
		defer qc.Close()

		qc.Exec(query, rootNode)

		// Collect matches
		var matches []sitter.QueryMatch
		for {
			match, ok := qc.NextMatch()
			if !ok {
				break
			}
			matches = append(matches, *match)
		}

		if len(matches) == 0 {
			fmt.Println("No tags found")
		} else {
			fmt.Printf("Found %d tag(s):\n", len(matches))
			for i, match := range matches {
				fmt.Printf("  Match %d:\n", i+1)
				for _, capture := range match.Captures {
					captureName := query.CaptureNameForId(capture.Index)
					text := tc.code[capture.Node.StartByte():capture.Node.EndByte()]
					fmt.Printf("    %s: %s\n", captureName, text)
				}
			}
		}
	}
}
