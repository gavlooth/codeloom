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
	fmt.Println("Testing defclass tag queries...")

	// Test cases with defclass examples
	testCases := []struct {
		name string
		code string
	}{
		{
			name: "Simple defclass with base class",
			code: "(defclass my-class (base-class) ())",
		},
		{
			name: "Defclass with multiple base classes",
			code: "(defclass another-class (class-a class-b class-c) ())",
		},
		{
			name: "Defclass with slots",
			code: "(defclass person (object)\n  ((name :accessor person-name :initarg :name)\n   (age :accessor person-age :initarg :age)))",
		},
		{
			name: "Defclass with qualified class name",
			code: "(defclass my-package:my-class (my-package:base-class) ())",
		},
		{
			name: "Defclass with cl: prefix",
			code: "(defclass cl:my-class (cl:object) ())",
		},
		{
			name: "Function call (should still be tagged)",
			code: "(some-func arg1 arg2)",
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
