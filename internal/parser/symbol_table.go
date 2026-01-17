package parser

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// SymbolTable maps symbol names to their fully qualified IDs
// Implementations are language-specific and opt-in
type SymbolTable interface {
	// Register adds a symbol definition to the table
	Register(name string, id string, symbolType NodeType)

	// Resolve attempts to resolve a symbol reference to its full ID
	// Returns the resolved ID and true if found, or empty string and false if not
	Resolve(name string, context *ResolutionContext) (string, bool)

	// RegisterImport records an import/require statement
	RegisterImport(alias string, target string)
}

// ResolutionContext provides context for symbol resolution
type ResolutionContext struct {
	FilePath       string   // Current file
	CurrentFunc    string   // Current function/method being parsed
	PackageName    string   // Current package/namespace
	VisibleImports []string // Imports visible in current scope
}

// BaseSymbolTable provides common functionality for symbol tables
type BaseSymbolTable struct {
	// symbols maps name -> fully qualified ID
	symbols map[string]string

	// imports maps alias -> target (e.g., "fmt" -> "fmt", "u" -> "utils")
	imports map[string]string

	// packageSymbols maps package::name -> ID for cross-package resolution
	packageSymbols map[string]string
}

// NewBaseSymbolTable creates a new base symbol table
func NewBaseSymbolTable() *BaseSymbolTable {
	return &BaseSymbolTable{
		symbols:        make(map[string]string),
		imports:        make(map[string]string),
		packageSymbols: make(map[string]string),
	}
}

func (t *BaseSymbolTable) Register(name string, id string, symbolType NodeType) {
	t.symbols[name] = id
}

func (t *BaseSymbolTable) RegisterImport(alias string, target string) {
	t.imports[alias] = target
}

func (t *BaseSymbolTable) Resolve(name string, context *ResolutionContext) (string, bool) {
	// Direct lookup
	if id, ok := t.symbols[name]; ok {
		return id, true
	}
	return "", false
}

// EdgeExtractor handles extraction of call edges from AST
type EdgeExtractor struct {
	symbolTable SymbolTable // nil if not using symbol tables
}

// NewEdgeExtractor creates a new edge extractor
// symbolTable can be nil for generic extraction without resolution
func NewEdgeExtractor(symbolTable SymbolTable) *EdgeExtractor {
	return &EdgeExtractor{
		symbolTable: symbolTable,
	}
}

// ExtractEdges walks the AST and extracts call edges
// callerID is the ID of the containing function/method
func (e *EdgeExtractor) ExtractEdges(node *sitter.Node, callerID string, filePath string, content []byte, result *ParseResult) {
	if node == nil {
		return
	}

	nodeType := node.Type()

	// Check for call expressions
	switch nodeType {
	case "call_expression", "method_invocation", "invocation_expression":
		callee := e.extractCalleeName(node, content)
		if callee != "" && callerID != "" {
			toID := e.resolveCallee(callee, filePath)
			result.Edges = append(result.Edges, CodeEdge{
				FromID:   callerID,
				ToID:     toID,
				EdgeType: EdgeTypeCalls,
			})
		}

	case "import_declaration", "import_statement", "use_declaration", "include_directive":
		// Record imports for symbol resolution
		if e.symbolTable != nil {
			alias, target := e.extractImport(node, content)
			if alias != "" && target != "" {
				e.symbolTable.RegisterImport(alias, target)
			}
		}
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		e.ExtractEdges(child, callerID, filePath, content, result)
	}
}

// extractCalleeName extracts the name of the function being called
func (e *EdgeExtractor) extractCalleeName(node *sitter.Node, content []byte) string {
	// Try field name "function" first (common in many languages)
	if funcNode := node.ChildByFieldName("function"); funcNode != nil {
		return e.extractIdentifier(funcNode, content)
	}

	// Try field name "name"
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return e.extractIdentifier(nameNode, content)
	}

	// Try field name "method" (Java)
	if methodNode := node.ChildByFieldName("method"); methodNode != nil {
		return e.extractIdentifier(methodNode, content)
	}

	// Fallback: look for first identifier or selector child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		childType := child.Type()

		switch childType {
		case "identifier":
			return string(content[child.StartByte():child.EndByte()])

		case "selector_expression", "member_expression", "field_expression":
			// Handle pkg.Func or obj.Method
			return e.extractSelectorExpression(child, content)

		case "scoped_identifier", "qualified_identifier":
			// Handle Namespace::Func (C++, Rust)
			return string(content[child.StartByte():child.EndByte()])
		}
	}

	return ""
}

// extractSelectorExpression extracts "pkg.Func" style calls
func (e *EdgeExtractor) extractSelectorExpression(node *sitter.Node, content []byte) string {
	// Get the full text for now
	// Could be refined to extract just the final identifier
	text := string(content[node.StartByte():node.EndByte()])
	return text
}

// extractIdentifier recursively extracts identifier from complex nodes
func (e *EdgeExtractor) extractIdentifier(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	nodeType := node.Type()

	switch nodeType {
	case "identifier", "type_identifier":
		return string(content[node.StartByte():node.EndByte()])

	case "selector_expression", "member_expression", "field_expression":
		return e.extractSelectorExpression(node, content)

	case "scoped_identifier", "qualified_identifier":
		return string(content[node.StartByte():node.EndByte()])

	default:
		// Try to find identifier in children
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "identifier" {
				return string(content[child.StartByte():child.EndByte()])
			}
		}
	}

	return ""
}

// extractImport extracts import alias and target
func (e *EdgeExtractor) extractImport(node *sitter.Node, content []byte) (alias string, target string) {
	// Generic extraction - language-specific tables can override

	// Try to extract path/name
	if pathNode := node.ChildByFieldName("path"); pathNode != nil {
		target = strings.Trim(string(content[pathNode.StartByte():pathNode.EndByte()]), "\"'`")
		// Alias is usually the last component
		parts := strings.Split(target, "/")
		alias = parts[len(parts)-1]
	}

	if alias == "" {
		// Fallback: look for string literal
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "interpreted_string_literal" || child.Type() == "string" || child.Type() == "string_literal" {
				target = strings.Trim(string(content[child.StartByte():child.EndByte()]), "\"'`")
				parts := strings.Split(target, "/")
				alias = parts[len(parts)-1]
				break
			}
		}
	}

	return alias, target
}

// resolveCallee attempts to resolve a callee name to a full ID
func (e *EdgeExtractor) resolveCallee(callee string, filePath string) string {
	// If we have a symbol table, try to resolve
	if e.symbolTable != nil {
		ctx := &ResolutionContext{
			FilePath: filePath,
		}
		if resolved, ok := e.symbolTable.Resolve(callee, ctx); ok {
			return resolved
		}
	}

	// Check if it's already a qualified name (contains . or ::)
	if strings.Contains(callee, ".") || strings.Contains(callee, "::") {
		// Keep as-is but mark as potentially external
		return fmt.Sprintf("external::%s", callee)
	}

	// Unresolved - could be same file, could be external
	// Use file path as prefix for potential same-file resolution
	return fmt.Sprintf("%s::%s", filePath, callee)
}
