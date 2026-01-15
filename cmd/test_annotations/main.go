package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/heefoo/codeloom/internal/parser"
)

func main() {
	goCode := `package main

/*@semantic
id: function::greet
kind: function
name: greet
summary: Greets a user by name
inputs:
  - name: string — the user's name
outputs:
  - greeting: string — personalized greeting
*/
func greet(name string) string {
    return "Hello, " + name + "!"
}

// ProcessData handles incoming data streams
func ProcessData(data []byte) error {
    return nil
}
`

	cCode := `/*@semantic
id: function::init_system
kind: function
name: init_system
summary: Initializes the system components
side_effects:
  - memory
  - global_state
thread_safety:
  - not thread-safe
*/
int init_system(void) {
    return 0;
}
`

	pyCode := `def calculate_sum(numbers):
    """Calculate the sum of a list of numbers.

    This function takes a list and returns its total.
    """
    return sum(numbers)
`

	p := parser.NewParser()

	fmt.Println("=== Go Code ===")
	result, _ := p.ParseContent(context.Background(), "test.go", parser.LangGo, []byte(goCode))
	for _, node := range result.Nodes {
		if node.NodeType == parser.NodeTypeFunction {
			out, _ := json.MarshalIndent(map[string]interface{}{
				"name":        node.Name,
				"doc_comment": node.DocComment,
				"annotations": node.Annotations,
			}, "", "  ")
			fmt.Println(string(out))
		}
	}

	fmt.Println("\n=== C Code ===")
	result, _ = p.ParseContent(context.Background(), "test.c", parser.LangC, []byte(cCode))
	for _, node := range result.Nodes {
		if node.NodeType == parser.NodeTypeFunction {
			out, _ := json.MarshalIndent(map[string]interface{}{
				"name":        node.Name,
				"doc_comment": node.DocComment,
				"annotations": node.Annotations,
			}, "", "  ")
			fmt.Println(string(out))
		}
	}

	fmt.Println("\n=== Python Code ===")
	result, _ = p.ParseContent(context.Background(), "test.py", parser.LangPython, []byte(pyCode))
	for _, node := range result.Nodes {
		if node.NodeType == parser.NodeTypeFunction {
			out, _ := json.MarshalIndent(map[string]interface{}{
				"name":        node.Name,
				"doc_comment": node.DocComment,
				"annotations": node.Annotations,
			}, "", "  ")
			fmt.Println(string(out))
		}
	}

	// Test Clojure
	clojureCode := `;; @semantic
;; id: function::greet
;; kind: function
;; name: greet
;; summary: Greets a user by name
(defn greet [name]
  (str "Hello, " name "!"))

(defn add [a b]
  "Adds two numbers together."
  (+ a b))
`

	fmt.Println("\n=== Clojure Code ===")
	result, err := p.ParseContent(context.Background(), "test.clj", parser.LangClojure, []byte(clojureCode))
	if err != nil {
		fmt.Printf("Error parsing Clojure: %v\n", err)
	} else {
		for _, node := range result.Nodes {
			if node.NodeType == parser.NodeTypeFunction {
				out, _ := json.MarshalIndent(map[string]interface{}{
					"name":        node.Name,
					"doc_comment": node.DocComment,
					"annotations": node.Annotations,
				}, "", "  ")
				fmt.Println(string(out))
			}
		}
	}

	// Test Julia
	juliaCode := `#=@semantic
id: function::greet
kind: function
name: greet
summary: Greets a user by name
=#
function greet(name)
    return "Hello, " * name * "!"
end

"Adds two numbers together."
function add(a, b)
    return a + b
end
`

	fmt.Println("\n=== Julia Code ===")
	result, err = p.ParseContent(context.Background(), "test.jl", parser.LangJulia, []byte(juliaCode))
	if err != nil {
		fmt.Printf("Error parsing Julia: %v\n", err)
	} else {
		for _, node := range result.Nodes {
			if node.NodeType == parser.NodeTypeFunction {
				out, _ := json.MarshalIndent(map[string]interface{}{
					"name":        node.Name,
					"doc_comment": node.DocComment,
					"annotations": node.Annotations,
				}, "", "  ")
				fmt.Println(string(out))
			}
		}
	}
}
