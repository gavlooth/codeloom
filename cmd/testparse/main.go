package main

import (
	"context"
	"fmt"
	"github.com/heefoo/codeloom/internal/parser"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: testparse <file>")
		return
	}
	p := parser.NewParser()
	result, err := p.ParseFile(context.Background(), os.Args[1])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Nodes: %d\n", len(result.Nodes))
	fmt.Printf("Edges: %d\n", len(result.Edges))
	for i, e := range result.Edges {
		if i > 10 {
			fmt.Printf("... and %d more edges\n", len(result.Edges)-10)
			break
		}
		fmt.Printf("  %s -> %s\n", e.FromID, e.ToID)
	}
}
