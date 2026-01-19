package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/heefoo/codeloom/internal/parser"
	sitter "github.com/smacker/go-tree-sitter"
)

type TestCase struct {
	Input string
	Base  int
}

func main() {
	p := parser.NewParser()
	commonlispLang := p.GetLanguage(parser.LangCommonLisp)
	commonlispParser := sitter.NewParser()
	defer commonlispParser.Close()
	commonlispParser.SetLanguage(commonlispLang)

	// Test valid radix numbers (bases 2-36)
	validTests := []TestCase{
		{"#2r1010", 2},
		{"#8r17", 8},
		{"#10r256", 10},
		{"#16rFF", 16},
		{"#36rZ", 36},
		{"#3r210", 3},
		{"#7r16", 7},
		{"#11rA", 11},
		{"#20rJ", 20},
		{"#30rN", 30},
		{"#35rZ", 35},
	}

	// Test invalid radix numbers (bases < 2 or > 36)
	invalidTests := []TestCase{
		{"#37r10", 37},
		{"#40r10", 40},
		{"#99r1", 99},
		{"#0r0", 0},
		{"#1r0", 1},
	}

	fmt.Println("=== Valid Radix Numbers (Base 2-36) ===")
	for i, test := range validTests {
		testNumber := i + 1
		input := test.Input

		ctx := context.Background()
		tree, err := commonlispParser.ParseCtx(ctx, nil, []byte(input))
		if err != nil {
			fmt.Printf("Test %2d: %-8s (base %2d) -> PARSE ERROR: %v\n", testNumber, test.Input, test.Base, err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		status := ""

		singleNumLit := rootNode.ChildCount() == 1 && rootNode.Child(0).Type() == "num_lit"
		if singleNumLit {
			child := rootNode.Child(0)
			if int(child.EndByte()-child.StartByte()) == len(input) {
				status = "✓ NUM_LIT"
			} else {
				status = "✗ PARTIAL"
			}
		} else if rootNode.ChildCount() > 1 {
			status = "✗ SPLIT"
			children := []string{}
			for j := 0; j < int(rootNode.ChildCount()); j++ {
				children = append(children, rootNode.Child(j).Type())
			}
			status += fmt.Sprintf(" (%s)", strings.Join(children, " + "))
		} else {
			status = fmt.Sprintf("✗ %s", rootNode.Type())
		}

		if rootNode.HasError() {
			status = "✗ ERROR"
		}

		fmt.Printf("Test %2d: %-8s (base %2d) -> %s\n", testNumber, test.Input, test.Base, status)
	}

	fmt.Println("\n=== Invalid Radix Numbers (Base < 2 or > 36) ===")
	for i, test := range invalidTests {
		testNumber := len(validTests) + i + 1
		input := test.Input

		ctx := context.Background()
		tree, err := commonlispParser.ParseCtx(ctx, nil, []byte(input))
		if err != nil {
			fmt.Printf("Test %2d: %-8s (base %2d) -> PARSE ERROR: %v\n", testNumber, test.Input, test.Base, err)
			continue
		}
		defer tree.Close()

		rootNode := tree.RootNode()
		status := ""

		// For invalid bases, we expect it to NOT parse as a single num_lit node
		singleNumLit := rootNode.ChildCount() == 1 && rootNode.Child(0).Type() == "num_lit"
		if singleNumLit {
			child := rootNode.Child(0)
			if int(child.EndByte()-child.StartByte()) == len(input) {
				status = "✗ ACCEPTED (should reject)"
			} else {
				status = "✓ PARTIAL"
			}
		} else if rootNode.ChildCount() > 1 {
			status = "✓ SPLIT"
			children := []string{}
			for j := 0; j < int(rootNode.ChildCount()); j++ {
				children = append(children, rootNode.Child(j).Type())
			}
			status += fmt.Sprintf(" (%s)", strings.Join(children, " + "))
		} else {
			status = fmt.Sprintf("✓ %s", rootNode.Type())
		}

		if rootNode.HasError() {
			status = "✓ ERROR"
		}

		fmt.Printf("Test %2d: %-8s (base %2d) -> %s\n", testNumber, test.Input, test.Base, status)
	}
}
