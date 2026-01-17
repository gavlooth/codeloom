package parser

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// CSymbolTable handles C/C++ specific symbol resolution
type CSymbolTable struct {
	*BaseSymbolTable

	// includes maps header name -> file path (if known)
	includes map[string]string

	// fileSymbols maps file -> symbols defined in that file
	fileSymbols map[string]map[string]string

	// currentFile tracks the file being processed
	currentFile string
}

// NewCSymbolTable creates a new C symbol table
func NewCSymbolTable() *CSymbolTable {
	return &CSymbolTable{
		BaseSymbolTable: NewBaseSymbolTable(),
		includes:        make(map[string]string),
		fileSymbols:     make(map[string]map[string]string),
	}
}

// SetCurrentFile sets the current file context
func (t *CSymbolTable) SetCurrentFile(path string) {
	t.currentFile = path
	if _, ok := t.fileSymbols[path]; !ok {
		t.fileSymbols[path] = make(map[string]string)
	}
}

func (t *CSymbolTable) Register(name string, id string, symbolType NodeType) {
	t.BaseSymbolTable.Register(name, id, symbolType)

	// Also register in file-level map
	if t.currentFile != "" {
		if _, ok := t.fileSymbols[t.currentFile]; !ok {
			t.fileSymbols[t.currentFile] = make(map[string]string)
		}
		t.fileSymbols[t.currentFile][name] = id
	}
}

func (t *CSymbolTable) RegisterImport(alias string, target string) {
	t.BaseSymbolTable.RegisterImport(alias, target)
	t.includes[alias] = target
}

func (t *CSymbolTable) Resolve(name string, context *ResolutionContext) (string, bool) {
	// Check for scoped name (Namespace::Func in C++)
	if strings.Contains(name, "::") {
		parts := strings.Split(name, "::")
		// Try to resolve the full qualified name
		fullName := strings.Join(parts, "::")
		if id, ok := t.symbols[fullName]; ok {
			return id, true
		}
		// Return as-is if not found
		return fmt.Sprintf("external::%s", name), true
	}

	// Check for member access (obj.method or ptr->method)
	// These are typically method calls, harder to resolve statically
	if strings.Contains(name, ".") || strings.Contains(name, "->") {
		return fmt.Sprintf("external::%s", name), false
	}

	// Local lookup
	if id, ok := t.symbols[name]; ok {
		return id, true
	}

	// Check current file's symbols
	if context != nil && context.FilePath != "" {
		if fileSyms, ok := t.fileSymbols[context.FilePath]; ok {
			if id, ok := fileSyms[name]; ok {
				return id, true
			}
		}
	}

	// Check included files
	for _, includePath := range t.includes {
		if fileSyms, ok := t.fileSymbols[includePath]; ok {
			if id, ok := fileSyms[name]; ok {
				return id, true
			}
		}
	}

	return "", false
}

// BuildFromAST builds symbol table from a C/C++ AST
func (t *CSymbolTable) BuildFromAST(root *sitter.Node, filePath string, content []byte) {
	t.SetCurrentFile(filePath)
	t.walkAndRegister(root, filePath, content)
}

func (t *CSymbolTable) walkAndRegister(node *sitter.Node, filePath string, content []byte) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "preproc_include":
		t.handleInclude(node, content)

	case "function_definition":
		t.handleFunctionDefinition(node, filePath, content)

	case "declaration":
		t.handleDeclaration(node, filePath, content)

	case "struct_specifier", "class_specifier", "enum_specifier":
		t.handleTypeDefinition(node, filePath, content)

	case "namespace_definition":
		// C++ namespace
		t.handleNamespace(node, filePath, content)
	}

	// Recurse
	for i := 0; i < int(node.ChildCount()); i++ {
		t.walkAndRegister(node.Child(i), filePath, content)
	}
}

func (t *CSymbolTable) handleInclude(node *sitter.Node, content []byte) {
	// #include "file.h" or #include <file.h>
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "string_literal":
			// "file.h"
			path := strings.Trim(string(content[child.StartByte():child.EndByte()]), "\"")
			name := filepath.Base(path)
			t.RegisterImport(name, path)

		case "system_lib_string":
			// <file.h>
			path := strings.Trim(string(content[child.StartByte():child.EndByte()]), "<>")
			name := filepath.Base(path)
			t.RegisterImport(name, path)
		}
	}
}

func (t *CSymbolTable) handleFunctionDefinition(node *sitter.Node, filePath string, content []byte) {
	// Find declarator which contains the function name
	declarator := node.ChildByFieldName("declarator")
	if declarator == nil {
		// Try to find it manually
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if strings.Contains(child.Type(), "declarator") {
				declarator = child
				break
			}
		}
	}

	if declarator != nil {
		name := t.extractDeclaratorName(declarator, content)
		if name != "" {
			id := fmt.Sprintf("%s::%s", filePath, name)
			t.Register(name, id, NodeTypeFunction)
		}
	}
}

func (t *CSymbolTable) handleDeclaration(node *sitter.Node, filePath string, content []byte) {
	// Could be function declaration or variable declaration
	// Check if it has a function declarator
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "function_declarator" {
			name := t.extractDeclaratorName(child, content)
			if name != "" {
				id := fmt.Sprintf("%s::%s", filePath, name)
				t.Register(name, id, NodeTypeFunction)
			}
		} else if child.Type() == "init_declarator" {
			// Variable declaration
			declNode := child.ChildByFieldName("declarator")
			if declNode != nil {
				name := t.extractDeclaratorName(declNode, content)
				if name != "" {
					id := fmt.Sprintf("%s::%s", filePath, name)
					t.Register(name, id, NodeTypeVariable)
				}
			}
		}
	}
}

func (t *CSymbolTable) handleTypeDefinition(node *sitter.Node, filePath string, content []byte) {
	// struct Foo { ... }, class Foo { ... }, enum Foo { ... }
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		name := string(content[nameNode.StartByte():nameNode.EndByte()])
		id := fmt.Sprintf("%s::%s", filePath, name)
		t.Register(name, id, NodeTypeClass)
	}
}

func (t *CSymbolTable) handleNamespace(node *sitter.Node, filePath string, content []byte) {
	// namespace Foo { ... }
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		name := string(content[nameNode.StartByte():nameNode.EndByte()])
		id := fmt.Sprintf("%s::%s", filePath, name)
		t.Register(name, id, NodeTypeModule)
	}
}

func (t *CSymbolTable) extractDeclaratorName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	switch node.Type() {
	case "identifier":
		return string(content[node.StartByte():node.EndByte()])

	case "function_declarator":
		// Look for declarator field or first identifier
		if declNode := node.ChildByFieldName("declarator"); declNode != nil {
			return t.extractDeclaratorName(declNode, content)
		}
		// Fallback: find identifier child
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "identifier" {
				return string(content[child.StartByte():child.EndByte()])
			}
			if strings.Contains(child.Type(), "declarator") {
				name := t.extractDeclaratorName(child, content)
				if name != "" {
					return name
				}
			}
		}

	case "pointer_declarator", "array_declarator", "parenthesized_declarator":
		// Unwrap: *foo, foo[], (foo)
		if declNode := node.ChildByFieldName("declarator"); declNode != nil {
			return t.extractDeclaratorName(declNode, content)
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "identifier" {
				return string(content[child.StartByte():child.EndByte()])
			}
			if strings.Contains(child.Type(), "declarator") {
				name := t.extractDeclaratorName(child, content)
				if name != "" {
					return name
				}
			}
		}

	case "scoped_identifier", "qualified_identifier":
		// Namespace::Func
		return string(content[node.StartByte():node.EndByte()])

	case "field_identifier":
		return string(content[node.StartByte():node.EndByte()])
	}

	return ""
}

// CEdgeExtractor handles C/C++ specific edge extraction
type CEdgeExtractor struct {
	*EdgeExtractor
	symbolTable *CSymbolTable
}

// NewCEdgeExtractor creates a C-specific edge extractor
func NewCEdgeExtractor(symbolTable *CSymbolTable) *CEdgeExtractor {
	return &CEdgeExtractor{
		EdgeExtractor: NewEdgeExtractor(symbolTable),
		symbolTable:   symbolTable,
	}
}

// ExtractEdges extracts call edges from C/C++ code
func (e *CEdgeExtractor) ExtractEdges(node *sitter.Node, callerID string, filePath string, content []byte, result *ParseResult) {
	if node == nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case "call_expression":
		if callerID != "" {
			callee := e.extractCCallee(node, content)
			if callee != "" {
				toID := e.resolveCCallee(callee, filePath)
				result.Edges = append(result.Edges, CodeEdge{
					FromID:   callerID,
					ToID:     toID,
					EdgeType: EdgeTypeCalls,
				})
			}
		}

	case "preproc_include":
		// Record include as an import edge
		header := e.extractIncludePath(node, content)
		if header != "" {
			result.Edges = append(result.Edges, CodeEdge{
				FromID:   filePath,
				ToID:     fmt.Sprintf("header::%s", header),
				EdgeType: EdgeTypeImports,
			})
		}
	}

	// Recurse
	for i := 0; i < int(node.ChildCount()); i++ {
		e.ExtractEdges(node.Child(i), callerID, filePath, content, result)
	}
}

func (e *CEdgeExtractor) extractCCallee(node *sitter.Node, content []byte) string {
	// call_expression has "function" field
	if funcNode := node.ChildByFieldName("function"); funcNode != nil {
		switch funcNode.Type() {
		case "identifier":
			return string(content[funcNode.StartByte():funcNode.EndByte()])

		case "field_expression":
			// obj.method or ptr->method
			if fieldNode := funcNode.ChildByFieldName("field"); fieldNode != nil {
				return string(content[fieldNode.StartByte():fieldNode.EndByte()])
			}
			return string(content[funcNode.StartByte():funcNode.EndByte()])

		case "scoped_identifier":
			// Namespace::Func
			return string(content[funcNode.StartByte():funcNode.EndByte()])

		default:
			// Fallback to full text
			return string(content[funcNode.StartByte():funcNode.EndByte()])
		}
	}

	// Fallback: look for identifier child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" {
			return string(content[child.StartByte():child.EndByte()])
		}
	}

	return ""
}

func (e *CEdgeExtractor) extractIncludePath(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "string_literal":
			return strings.Trim(string(content[child.StartByte():child.EndByte()]), "\"")
		case "system_lib_string":
			return strings.Trim(string(content[child.StartByte():child.EndByte()]), "<>")
		}
	}
	return ""
}

func (e *CEdgeExtractor) resolveCCallee(callee string, filePath string) string {
	if e.symbolTable != nil {
		ctx := &ResolutionContext{FilePath: filePath}
		if resolved, ok := e.symbolTable.Resolve(callee, ctx); ok {
			return resolved
		}
	}

	// Check for qualified call
	if strings.Contains(callee, "::") || strings.Contains(callee, ".") || strings.Contains(callee, "->") {
		return fmt.Sprintf("external::%s", callee)
	}

	return fmt.Sprintf("%s::%s", filePath, callee)
}
