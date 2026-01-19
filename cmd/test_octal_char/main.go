package main

import (
	"fmt"
	"os"

	"github.com/heefoo/codeloom/internal/parser/grammars/clojure_lang"
	sitter "github.com/smacker/go-tree-sitter"
)

func main() {
	lang := clojure_lang.GetLanguage()
	parser := sitter.NewParser()
	defer parser.Close()
	parser.SetLanguage(lang)

	testCases := []struct {
		input    string
		expected string // "pass" for valid, "partial" for partial match, "fail" for no char_lit
		desc     string
	}{
		// Valid octal characters (digits 0-7) - should match completely
		{`\o0`, "pass", "Octal 0"},
		{`\o7`, "pass", "Octal 7"},
		{`\o77`, "pass", "Octal 77"},
		{`\o377`, "pass", "Octal 377 (max 8-bit)"},
		{`\o12`, "pass", "Octal 12"},
		{`\o001`, "pass", "Octal 001 with leading zeros"},

		// Invalid octal characters (digits 8-9) - should match valid prefix only
		{`\o8`, "partial", "Invalid octal digit 8 (matches \\o only)"},
		{`\o9`, "partial", "Invalid octal digit 9 (matches \\o only)"},
		{`\o38`, "partial", "Invalid octal 38 (matches \\o3)"},
		{`\o378`, "partial", "Invalid octal 378 (matches \\o37)"},
		{`\o789`, "partial", "Invalid octal 789 (matches \\o7)"},
		{`\o400`, "pass", "Valid octal 400 (octal number)"},

		// Other character literals (should still pass)
		{`\a`, "pass", "Simple char"},
		{`\backspace`, "pass", "Named char"},
		{`\u611B`, "pass", "Unicode char"},
	}

	allPassed := true
	for i, tc := range testCases {
		tree := parser.Parse(nil, []byte(tc.input))
		if tree == nil {
			fmt.Printf("Test %d (%s): Parse returned nil\n", i, tc.desc)
			allPassed = false
			continue
		}

		rootNode := tree.RootNode()
		charLitNode := findCharLitNode(rootNode)

		// Determine if test passed
		var passed bool
		var parseResult string

		if tc.expected == "pass" {
			// Should fully match input as a single char_lit
			passed = (charLitNode != nil &&
				charLitNode.StartByte() == 0 &&
				charLitNode.EndByte() == uint32(len(tc.input)))
			if passed {
				parseResult = "full char_lit match"
			} else {
				parseResult = fmt.Sprintf("char_lit not covering full input (got %d:%d, expected 0:%d)",
					charLitNode.StartByte(), charLitNode.EndByte(), len(tc.input))
			}
		} else if tc.expected == "partial" {
			// Should have char_lit but not covering full input (due to invalid octal digits)
			passed = (charLitNode != nil &&
				charLitNode.StartByte() == 0 &&
				charLitNode.EndByte() < uint32(len(tc.input)))
			if passed {
				parseResult = fmt.Sprintf("partial char_lit match (0:%d of 0:%d)",
					charLitNode.EndByte(), len(tc.input))
			} else if charLitNode == nil {
				parseResult = "no char_lit found"
			} else {
				parseResult = fmt.Sprintf("unexpected: char_lit covers 0:%d of 0:%d",
					charLitNode.EndByte(), len(tc.input))
			}
		} else {
			// Should have no char_lit
			passed = (charLitNode == nil)
			if passed {
				parseResult = "no char_lit (correct)"
			} else {
				parseResult = "char_lit found but should not be"
			}
		}

		// Print diagnostic for partial matches
		if tc.expected == "partial" && charLitNode != nil {
			fmt.Printf("\n=== Parse tree for '%s' ===\n", tc.input)
			printParseTree(rootNode, []byte(tc.input), 0)
			fmt.Printf("==============================\n\n")
		}

		status := "✓"
		if !passed {
			status = "✗"
			allPassed = false
		}

		fmt.Printf("%s Test %d: %-35s Input: %-10s Expected: %-7s Got: %s\n",
			status, i, tc.desc, tc.input, tc.expected, parseResult)
	}

	if allPassed {
		fmt.Println("\n✓ All tests passed!")
		os.Exit(0)
	} else {
		fmt.Println("\n✗ Some tests failed!")
		os.Exit(1)
	}
}

func findCharLitNode(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}

	if node.Type() == "char_lit" {
		return node
	}

	for i := 0; i < int(int(node.ChildCount())); i++ {
		if result := findCharLitNode(node.Child(i)); result != nil {
			return result
		}
	}

	return nil
}

func findCharLit(node *sitter.Node) bool {
	return findCharLitNode(node) != nil
}

func printParseTree(node *sitter.Node, content []byte, indent int) {
	if node == nil {
		return
	}

	indentStr := ""
	for i := 0; i < indent; i++ {
		indentStr += "  "
	}

	nodeContent := node.Content(content)
	if len(nodeContent) > 20 {
		nodeContent = nodeContent[:20] + "..."
	}

	fmt.Printf("%s%s (%s) [%d:%d]\n", indentStr, node.Type(), nodeContent, node.StartByte(), node.EndByte())

	for i := 0; i < int(int(node.ChildCount())); i++ {
		printParseTree(node.Child(i), content, indent+1)
	}
}
