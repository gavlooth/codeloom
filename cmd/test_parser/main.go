package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/heefoo/codeloom/internal/parser"
)

func main() {
	p := parser.NewParser()
	result, err := p.ParseFile(context.Background(), "/tmp/test_annotations.go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, node := range result.Nodes {
		out, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(out))
		fmt.Println("---")
	}
}
