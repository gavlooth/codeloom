package main

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/heefoo/codeloom/internal/parser"
)

func printTree(node *sitter.Node, content []byte, indent string) {
	nodeType := node.Type()
	text := ""
	if node.ChildCount() == 0 {
		text = string(content[node.StartByte():node.EndByte()])
		if len(text) > 30 {
			text = text[:30] + "..."
		}
	}
	fmt.Printf("%s%s", indent, nodeType)
	if text != "" {
		fmt.Printf(" = %q", text)
	}
	fmt.Println()

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		printTree(child, content, indent+"  ")
	}
}

func main() {
	p := parser.NewParser()

	// Clojure
	fmt.Println("=== Clojure AST ===")
	clojureCode := []byte(`(defn greet [name]
  (str "Hello, " name "!"))`)
	clojureLang := p.GetLanguage(parser.LangClojure)
	clojureParser := sitter.NewParser()
	clojureParser.SetLanguage(clojureLang)
	tree, _ := clojureParser.ParseCtx(context.Background(), nil, clojureCode)
	printTree(tree.RootNode(), clojureCode, "")
	tree.Close()

	// Julia
	fmt.Println("\n=== Julia AST ===")
	juliaCode := []byte(`function greet(name)
    return "Hello, " * name * "!"
end`)
	juliaLang := p.GetLanguage(parser.LangJulia)
	juliaParser := sitter.NewParser()
	juliaParser.SetLanguage(juliaLang)
	tree, _ = juliaParser.ParseCtx(context.Background(), nil, juliaCode)
	printTree(tree.RootNode(), juliaCode, "")
	tree.Close()
}
