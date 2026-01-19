package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

type Tag struct {
	Name  string
	Type  string
	Start int
	End   int
}

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
	}

	p := parser.NewParser()
	clLang := p.GetLanguage(parser.LangCommonLisp)
	clParser := sitter.NewParser()
	defer clParser.Close()
	clParser.SetLanguage(clLang)

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

		// Print parse tree
		fmt.Printf("Parse tree structure:\n%s\n", formatNode(rootNode, tc.code, 0))

		// Extract tags
		tags := extractTags(rootNode, tc.code)
		if len(tags) == 0 {
			fmt.Println("\nNo tags found (this is expected before fix)")
		} else {
			fmt.Println("\nTags:")
			for _, tag := range tags {
				text := tc.code[tag.Start:tag.End]
				fmt.Printf("  %s: %s (%d:%d)\n", tag.Type, text, tag.Start, tag.End)
			}
		}
	}
}

func extractTags(node *sitter.Node, code string) []Tag {
	var tags []Tag

	// Walk the tree and look for patterns that match tag queries
	// For now, just look for symbols in certain contexts
	if node.Type() == "list_lit" {
		if node.ChildCount() >= 3 {
			// Check if first child is a symbol matching "defclass"
			if firstChild := node.Child(0); firstChild != nil {
				if firstChild.Type() == "sym_lit" {
					text := code[firstChild.StartByte():firstChild.EndByte()]
					if strings.EqualFold(text, "defclass") {
						// This is a defclass form
						// The class name is the second child
						if secondChild := node.Child(1); secondChild != nil {
							tags = append(tags, Tag{
								Name:  "my-class",
								Type:  "definition.class",
								Start: int(secondChild.StartByte()),
								End:   int(secondChild.EndByte()),
							})
						}
					}
				}
			}
		}
	}

	// Recursively check children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		tags = append(tags, extractTags(child, code)...)
	}

	return tags
}

func formatNode(node *sitter.Node, code string, indent int) string {
	var b strings.Builder
	indentStr := strings.Repeat("  ", indent)

	b.WriteString(fmt.Sprintf("%s%s [%d:%d]", indentStr, node.Type(), node.StartByte(), node.EndByte()))

	if node.ChildCount() > 0 {
		b.WriteString("\n")
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			b.WriteString(formatNode(child, code, indent+1))
		}
	}

	return b.String()
}
