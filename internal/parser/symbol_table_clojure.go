package parser

import (
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// ClojureSymbolTable handles Clojure-specific symbol resolution
type ClojureSymbolTable struct {
	*BaseSymbolTable

	// namespace is the current ns
	namespace string

	// file -> namespace mapping
	fileNamespaces map[string]string

	// namespace -> symbols mapping
	namespaceDefs map[string]map[string]string

	// alias -> full namespace (from :as clauses)
	namespaceAliases map[string]string

	// referred symbols (from :refer clauses) - symbol -> namespace
	referredSymbols map[string]string
}

// NewClojureSymbolTable creates a new Clojure symbol table
func NewClojureSymbolTable() *ClojureSymbolTable {
	return &ClojureSymbolTable{
		BaseSymbolTable:  NewBaseSymbolTable(),
		fileNamespaces:   make(map[string]string),
		namespaceDefs:    make(map[string]map[string]string),
		namespaceAliases: make(map[string]string),
		referredSymbols:  make(map[string]string),
	}
}

// SetNamespace sets the current namespace context
func (t *ClojureSymbolTable) SetNamespace(ns string) {
	t.namespace = ns
}

func (t *ClojureSymbolTable) Register(name string, id string, symbolType NodeType) {
	t.BaseSymbolTable.Register(name, id, symbolType)

	// Also register in namespace-level map
	if t.namespace != "" {
		if _, ok := t.namespaceDefs[t.namespace]; !ok {
			t.namespaceDefs[t.namespace] = make(map[string]string)
		}
		t.namespaceDefs[t.namespace][name] = id
	}
}

func (t *ClojureSymbolTable) RegisterImport(alias string, target string) {
	t.BaseSymbolTable.RegisterImport(alias, target)
	t.namespaceAliases[alias] = target
}

// RegisterRefer records a :refer'ed symbol
func (t *ClojureSymbolTable) RegisterRefer(symbol string, namespace string) {
	t.referredSymbols[symbol] = namespace
}

func (t *ClojureSymbolTable) Resolve(name string, context *ResolutionContext) (string, bool) {
	// Check for qualified name (ns/symbol)
	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		nsAlias := parts[0]
		symbolName := parts[1]

		// Look up the namespace alias
		fullNs := nsAlias
		if resolved, ok := t.namespaceAliases[nsAlias]; ok {
			fullNs = resolved
		}

		// Try to find in that namespace's definitions
		if nsDefs, ok := t.namespaceDefs[fullNs]; ok {
			if id, ok := nsDefs[symbolName]; ok {
				return id, true
			}
		}

		// Return as external reference
		return fmt.Sprintf("%s::%s", fullNs, symbolName), true
	}

	// Check if it's a referred symbol
	if ns, ok := t.referredSymbols[name]; ok {
		if nsDefs, ok := t.namespaceDefs[ns]; ok {
			if id, ok := nsDefs[name]; ok {
				return id, true
			}
		}
		return fmt.Sprintf("%s::%s", ns, name), true
	}

	// Local lookup - same namespace
	if id, ok := t.symbols[name]; ok {
		return id, true
	}

	// Check current namespace definitions
	if t.namespace != "" {
		if nsDefs, ok := t.namespaceDefs[t.namespace]; ok {
			if id, ok := nsDefs[name]; ok {
				return id, true
			}
		}
	}

	return "", false
}

// BuildFromAST builds symbol table from a Clojure AST
func (t *ClojureSymbolTable) BuildFromAST(root *sitter.Node, filePath string, content []byte) {
	t.walkAndRegister(root, filePath, content)
}

func (t *ClojureSymbolTable) walkAndRegister(node *sitter.Node, filePath string, content []byte) {
	if node == nil {
		return
	}

	// Clojure AST: list nodes contain function calls
	// (ns foo.bar) -> list with sym_lit "ns" and sym_lit "foo.bar"
	// (defn name ...) -> list with sym_lit "defn" and sym_lit "name"

	if node.Type() == "list_lit" {
		t.handleClojureList(node, filePath, content)
	}

	// Recurse
	for i := 0; i < int(node.ChildCount()); i++ {
		t.walkAndRegister(node.Child(i), filePath, content)
	}
}

func (t *ClojureSymbolTable) handleClojureList(node *sitter.Node, filePath string, content []byte) {
	// Get first symbol (the function being called)
	firstSym := t.getFirstSymbol(node, content)
	if firstSym == "" {
		return
	}

	switch firstSym {
	case "ns":
		// (ns foo.bar ...)
		t.handleNsDeclaration(node, filePath, content)

	case "defn", "defn-", "defmacro", "defmulti":
		// (defn name ...)
		t.handleDefn(node, filePath, content, firstSym)

	case "def", "defonce":
		// (def name ...)
		t.handleDef(node, filePath, content)

	case "require", ":require":
		// Inside ns or standalone
		t.handleRequire(node, content)
	}
}

func (t *ClojureSymbolTable) getFirstSymbol(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "sym_lit" || child.Type() == "kwd_lit" {
			text := string(content[child.StartByte():child.EndByte()])
			return text
		}
	}
	return ""
}

func (t *ClojureSymbolTable) getSecondSymbol(node *sitter.Node, content []byte) string {
	count := 0
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "sym_lit" {
			count++
			if count == 2 {
				return string(content[child.StartByte():child.EndByte()])
			}
		}
	}
	return ""
}

func (t *ClojureSymbolTable) handleNsDeclaration(node *sitter.Node, filePath string, content []byte) {
	// (ns foo.bar (:require ...))
	nsName := t.getSecondSymbol(node, content)
	if nsName != "" {
		t.namespace = nsName
		t.fileNamespaces[filePath] = nsName
		if _, ok := t.namespaceDefs[nsName]; !ok {
			t.namespaceDefs[nsName] = make(map[string]string)
		}
	}

	// Process :require clauses inside ns
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "list_lit" {
			firstSym := t.getFirstSymbol(child, content)
			if firstSym == ":require" || firstSym == "require" {
				t.handleRequire(child, content)
			}
		}
	}
}

func (t *ClojureSymbolTable) handleDefn(node *sitter.Node, filePath string, content []byte, defType string) {
	name := t.getSecondSymbol(node, content)
	if name != "" {
		id := fmt.Sprintf("%s::%s", filePath, name)
		nodeType := NodeTypeFunction
		if defType == "defmacro" {
			nodeType = NodeTypeMacro
		}
		t.Register(name, id, nodeType)
	}
}

func (t *ClojureSymbolTable) handleDef(node *sitter.Node, filePath string, content []byte) {
	name := t.getSecondSymbol(node, content)
	if name != "" {
		id := fmt.Sprintf("%s::%s", filePath, name)
		t.Register(name, id, NodeTypeVariable)
	}
}

func (t *ClojureSymbolTable) handleRequire(node *sitter.Node, content []byte) {
	// (:require [foo.bar :as fb] [baz.qux :refer [x y]])
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)

		switch child.Type() {
		case "vec_lit":
			// [foo.bar :as fb]
			t.handleRequireVector(child, content)

		case "sym_lit":
			// Plain namespace require
			ns := string(content[child.StartByte():child.EndByte()])
			if ns != "" && ns != "require" && !strings.HasPrefix(ns, ":") {
				// Use last component as alias
				parts := strings.Split(ns, ".")
				alias := parts[len(parts)-1]
				t.RegisterImport(alias, ns)
			}
		}
	}
}

func (t *ClojureSymbolTable) handleRequireVector(node *sitter.Node, content []byte) {
	// [foo.bar :as fb :refer [x y]]
	var namespace string
	var alias string
	var refers []string

	expectAlias := false
	expectRefers := false

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		text := string(content[child.StartByte():child.EndByte()])

		switch child.Type() {
		case "sym_lit":
			if expectAlias {
				alias = text
				expectAlias = false
			} else if namespace == "" {
				namespace = text
			}

		case "kwd_lit":
			if text == ":as" {
				expectAlias = true
			} else if text == ":refer" {
				expectRefers = true
			}

		case "vec_lit":
			if expectRefers {
				// [:refer [x y z]]
				for j := 0; j < int(child.ChildCount()); j++ {
					sym := child.Child(j)
					if sym.Type() == "sym_lit" {
						refers = append(refers, string(content[sym.StartByte():sym.EndByte()]))
					}
				}
				expectRefers = false
			}
		}
	}

	if namespace != "" {
		// Register alias
		if alias == "" {
			parts := strings.Split(namespace, ".")
			alias = parts[len(parts)-1]
		}
		t.RegisterImport(alias, namespace)

		// Register referred symbols
		for _, ref := range refers {
			t.RegisterRefer(ref, namespace)
		}
	}
}

// ClojureEdgeExtractor extends EdgeExtractor for Clojure-specific call detection
type ClojureEdgeExtractor struct {
	*EdgeExtractor
	symbolTable *ClojureSymbolTable
	seen        map[uint32]bool // Track visited nodes by start byte
}

// NewClojureEdgeExtractor creates a Clojure-specific edge extractor
func NewClojureEdgeExtractor(symbolTable *ClojureSymbolTable) *ClojureEdgeExtractor {
	return &ClojureEdgeExtractor{
		EdgeExtractor: NewEdgeExtractor(symbolTable),
		symbolTable:   symbolTable,
		seen:          make(map[uint32]bool),
	}
}

// ExtractEdges extracts call edges from Clojure code
func (e *ClojureEdgeExtractor) ExtractEdges(node *sitter.Node, callerID string, filePath string, content []byte, result *ParseResult) {
	// Reset seen map for each function extraction
	e.seen = make(map[uint32]bool)
	e.extractEdgesWithDepth(node, callerID, filePath, content, result, 0, 50) // Max depth 50
}

func (e *ClojureEdgeExtractor) extractEdgesWithDepth(node *sitter.Node, callerID string, filePath string, content []byte, result *ParseResult, depth, maxDepth int) {
	if node == nil || depth > maxDepth {
		return
	}

	// Skip if already processed (by start byte position)
	pos := node.StartByte()
	if e.seen[pos] {
		return
	}
	e.seen[pos] = true

	nodeType := node.Type()

	// In Clojure, function calls are list literals: (func arg1 arg2)
	if nodeType == "list_lit" && callerID != "" {
		callee := e.extractClojureCallee(node, content)
		if callee != "" && !isClojureSpecialForm(callee) {
			toID := e.resolveClojureCallee(callee, filePath)
			result.Edges = append(result.Edges, CodeEdge{
				FromID:   callerID,
				ToID:     toID,
				EdgeType: EdgeTypeCalls,
			})
		}
	}

	// Skip recursion into data literals that unlikely contain calls we care about
	// (quoted forms, regex, etc.)
	switch nodeType {
	case "regex_lit", "str_lit", "num_lit", "nil_lit", "bool_lit", "kwd_lit", "char_lit":
		return // No function calls in literals
	case "quoting_lit", "syn_quoting_lit":
		return // Quoted forms are data, not calls
	}

	// Recurse into children
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		e.extractEdgesWithDepth(node.Child(i), callerID, filePath, content, result, depth+1, maxDepth)
	}
}

func (e *ClojureEdgeExtractor) extractClojureCallee(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "sym_lit" {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

func (e *ClojureEdgeExtractor) resolveClojureCallee(callee string, filePath string) string {
	if e.symbolTable != nil {
		ctx := &ResolutionContext{FilePath: filePath}
		if resolved, ok := e.symbolTable.Resolve(callee, ctx); ok {
			return resolved
		}
	}

	// Check for qualified call (ns/func)
	if strings.Contains(callee, "/") {
		return fmt.Sprintf("external::%s", callee)
	}

	return fmt.Sprintf("%s::%s", filePath, callee)
}

// isClojureSpecialForm returns true if the symbol is a special form or macro
// that shouldn't be recorded as a function call
func isClojureSpecialForm(sym string) bool {
	specialForms := map[string]bool{
		// Special forms
		"def": true, "if": true, "do": true, "let": true, "quote": true,
		"var": true, "fn": true, "loop": true, "recur": true, "throw": true,
		"try": true, "catch": true, "finally": true, "monitor-enter": true,
		"monitor-exit": true, "new": true, "set!": true, ".": true,
		// Common macros that are structural
		"ns": true, "defn": true, "defn-": true, "defmacro": true,
		"defonce": true, "defmulti": true, "defmethod": true,
		"when": true, "when-not": true, "when-let": true, "when-first": true,
		"if-let": true, "if-not": true, "cond": true, "condp": true, "case": true,
		"and": true, "or": true, "not": true,
		"for": true, "doseq": true, "dotimes": true, "while": true,
		"->": true, "->>": true, "as->": true, "some->": true, "some->>": true,
		"require": true, ":require": true, "import": true, ":import": true,
		"use": true, ":use": true,
		"comment": true, "declare": true,
	}
	return specialForms[sym]
}
