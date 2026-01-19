package main

import (
	"context"
	"fmt"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	fmt.Println("Debugging radix parse tree structure...")

	p := parser.NewParser()
	clojureLang := p.GetLanguage(parser.LangClojure)
	clojureParser := sitter.NewParser()
	defer clojureParser.Close()
	clojureParser.SetLanguage(clojureLang)

	testCases := []string{
		"36rZ",        // Valid base 36
		"37r10",       // Invalid: base 37 > 36
		"40r10",       // Invalid: base 40 > 36
	}

	for _, code := range testCases {
		fmt.Printf("\n=== Testing: %s ===\n", code)
		ctx := context.Background()
		tree, err := clojureParser.ParseCtx(ctx, nil, []byte(code))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		fmt.Printf("Has error: %v\n", rootNode.HasError())
		fmt.Printf("Root node type: %s\n", rootNode.Type())
		fmt.Printf("Parse tree:\n%s\n", rootNode.String())

		if rootNode.ChildCount() > 0 {
			child := rootNode.Child(0)
			fmt.Printf("Child type: %s\n", child.Type())
			fmt.Printf("Child text: %q\n", string(code[child.StartByte():child.EndByte()]))
		}
	}
}
