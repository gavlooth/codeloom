package parser

import (
	"context"
	"testing"
)

func TestExtractDocCommentGo(t *testing.T) {
	code := `package main

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

	p := NewParser()
	result, err := p.ParseContent(context.Background(), "test.go", LangGo, []byte(code))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(result.Nodes) == 0 {
		t.Fatal("No nodes extracted")
	}

	// Find the greet function
	var greetNode *CodeNode
	var processNode *CodeNode
	for i := range result.Nodes {
		if result.Nodes[i].Name == "greet" {
			greetNode = &result.Nodes[i]
		}
		if result.Nodes[i].Name == "ProcessData" {
			processNode = &result.Nodes[i]
		}
	}

	if greetNode == nil {
		t.Fatal("greet function not found")
	}

	t.Logf("greet DocComment: %q", greetNode.DocComment)
	t.Logf("greet Annotations: %+v", greetNode.Annotations)

	// Check annotations were extracted
	if len(greetNode.Annotations) == 0 {
		t.Error("Expected annotations to be extracted for greet")
	}

	if greetNode.Annotations["id"] != "function::greet" {
		t.Errorf("Expected id='function::greet', got %q", greetNode.Annotations["id"])
	}

	if greetNode.Annotations["summary"] != "Greets a user by name" {
		t.Errorf("Expected summary='Greets a user by name', got %q", greetNode.Annotations["summary"])
	}

	if processNode == nil {
		t.Fatal("ProcessData function not found")
	}

	t.Logf("ProcessData DocComment: %q", processNode.DocComment)

	// Check doc comment was extracted
	if processNode.DocComment == "" {
		t.Error("Expected doc comment to be extracted for ProcessData")
	}
}

func TestExtractDocCommentPython(t *testing.T) {
	code := `"""Module docstring"""

def calculate_sum(numbers):
    """Calculate the sum of a list of numbers.

    This function takes a list and returns its total.
    """
    return sum(numbers)
`

	p := NewParser()
	result, err := p.ParseContent(context.Background(), "test.py", LangPython, []byte(code))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	var sumNode *CodeNode
	for i := range result.Nodes {
		if result.Nodes[i].Name == "calculate_sum" {
			sumNode = &result.Nodes[i]
			break
		}
	}

	if sumNode == nil {
		t.Fatal("calculate_sum function not found")
	}

	t.Logf("calculate_sum DocComment: %q", sumNode.DocComment)

	if sumNode.DocComment == "" {
		t.Error("Expected docstring to be extracted for calculate_sum")
	}
}

func TestExtractAnnotationsC(t *testing.T) {
	code := `/*@semantic
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

	p := NewParser()
	result, err := p.ParseContent(context.Background(), "test.c", LangC, []byte(code))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	var initNode *CodeNode
	for i := range result.Nodes {
		if result.Nodes[i].Name == "init_system" {
			initNode = &result.Nodes[i]
			break
		}
	}

	if initNode == nil {
		t.Fatal("init_system function not found")
	}

	t.Logf("init_system Annotations: %+v", initNode.Annotations)

	if initNode.Annotations["id"] != "function::init_system" {
		t.Errorf("Expected id='function::init_system', got %q", initNode.Annotations["id"])
	}

	if initNode.Annotations["summary"] != "Initializes the system components" {
		t.Errorf("Expected summary='Initializes the system components', got %q", initNode.Annotations["summary"])
	}
}
