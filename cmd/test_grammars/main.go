package main

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/heefoo/codeloom/internal/parser"
)

func main() {
	fmt.Println("Creating parser...")
	p := parser.NewParser()
	fmt.Println("Parser created successfully")

	// Check each language
	langs := []parser.Language{
		parser.LangGo,
		parser.LangC,
		parser.LangPython,
		parser.LangClojure,
		parser.LangJulia,
		parser.LangCommonLisp,
	}

	for _, lang := range langs {
		l := p.GetLanguage(lang)
		if l == nil {
			fmt.Printf("  %s: NOT REGISTERED\n", lang)
		} else {
			fmt.Printf("  %s: OK\n", lang)
		}
	}

	// Test parsing Clojure
	fmt.Println("\n=== Test Clojure Parsing ===")
	clojureLang := p.GetLanguage(parser.LangClojure)
	clojureParser := sitter.NewParser()
	clojureParser.SetLanguage(clojureLang)

	clojureCode := []byte(`(defn greet [name]
  (str "Hello, " name "!"))`)

	tree, err := clojureParser.ParseCtx(context.Background(), nil, clojureCode)
	if err != nil {
		fmt.Printf("Clojure parse error: %v\n", err)
	} else {
		fmt.Printf("Clojure AST: %s\n", tree.RootNode().String())
		tree.Close()
	}

	// Test parsing Julia
	fmt.Println("\n=== Test Julia Parsing ===")
	juliaLang := p.GetLanguage(parser.LangJulia)
	juliaParser := sitter.NewParser()
	juliaParser.SetLanguage(juliaLang)

	juliaCode := []byte(`function greet(name)
    return "Hello, " * name * "!"
end`)

	tree, err = juliaParser.ParseCtx(context.Background(), nil, juliaCode)
	if err != nil {
		fmt.Printf("Julia parse error: %v\n", err)
	} else {
		fmt.Printf("Julia AST: %s\n", tree.RootNode().String())
		tree.Close()
	}
}
