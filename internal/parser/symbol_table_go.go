package parser

import (
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// GoSymbolTable handles Go-specific symbol resolution
type GoSymbolTable struct {
	*BaseSymbolTable

	// packageName is the current package
	packageName string

	// filePath -> package mapping
	filePackages map[string]string

	// package -> []symbols mapping for cross-package resolution
	packageDefs map[string]map[string]string
}

// NewGoSymbolTable creates a new Go symbol table
func NewGoSymbolTable() *GoSymbolTable {
	return &GoSymbolTable{
		BaseSymbolTable: NewBaseSymbolTable(),
		filePackages:    make(map[string]string),
		packageDefs:     make(map[string]map[string]string),
	}
}

// SetPackage sets the current package context
func (t *GoSymbolTable) SetPackage(name string) {
	t.packageName = name
}

// RegisterFilePackage associates a file with its package
func (t *GoSymbolTable) RegisterFilePackage(filePath string, packageName string) {
	t.filePackages[filePath] = packageName
	if _, ok := t.packageDefs[packageName]; !ok {
		t.packageDefs[packageName] = make(map[string]string)
	}
}

func (t *GoSymbolTable) Register(name string, id string, symbolType NodeType) {
	t.BaseSymbolTable.Register(name, id, symbolType)

	// Also register in package-level map
	if t.packageName != "" {
		if _, ok := t.packageDefs[t.packageName]; !ok {
			t.packageDefs[t.packageName] = make(map[string]string)
		}
		t.packageDefs[t.packageName][name] = id
	}
}

func (t *GoSymbolTable) Resolve(name string, context *ResolutionContext) (string, bool) {
	// Check for qualified name (pkg.Func)
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		pkgAlias := parts[0]
		symbolName := parts[1]

		// Look up the import alias
		if pkgPath, ok := t.imports[pkgAlias]; ok {
			// Try to find in that package's definitions
			if pkgDefs, ok := t.packageDefs[pkgPath]; ok {
				if id, ok := pkgDefs[symbolName]; ok {
					return id, true
				}
			}
			// Return external reference
			return fmt.Sprintf("%s::%s", pkgPath, symbolName), true
		}

		// Could be a method call on a variable (obj.Method)
		// For now, return as unresolved method
		return fmt.Sprintf("%s::%s", context.FilePath, name), false
	}

	// Local lookup - same package
	if id, ok := t.symbols[name]; ok {
		return id, true
	}

	// Check current package definitions
	if context != nil && context.PackageName != "" {
		if pkgDefs, ok := t.packageDefs[context.PackageName]; ok {
			if id, ok := pkgDefs[name]; ok {
				return id, true
			}
		}
	}

	// Check file's package
	if context != nil && context.FilePath != "" {
		if pkgName, ok := t.filePackages[context.FilePath]; ok {
			if pkgDefs, ok := t.packageDefs[pkgName]; ok {
				if id, ok := pkgDefs[name]; ok {
					return id, true
				}
			}
		}
	}

	return "", false
}

// BuildFromAST builds symbol table from a Go AST
func (t *GoSymbolTable) BuildFromAST(root *sitter.Node, filePath string, content []byte) {
	t.walkAndRegister(root, filePath, content)
}

func (t *GoSymbolTable) walkAndRegister(node *sitter.Node, filePath string, content []byte) {
	if node == nil {
		return
	}

	switch node.Type() {
	case "package_clause":
		// Extract package name
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			t.packageName = string(content[nameNode.StartByte():nameNode.EndByte()])
			t.RegisterFilePackage(filePath, t.packageName)
		} else {
			// Fallback: look for identifier child
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "package_identifier" || child.Type() == "identifier" {
					t.packageName = string(content[child.StartByte():child.EndByte()])
					t.RegisterFilePackage(filePath, t.packageName)
					break
				}
			}
		}

	case "import_declaration":
		t.extractGoImports(node, content)

	case "function_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := string(content[nameNode.StartByte():nameNode.EndByte()])
			id := fmt.Sprintf("%s::%s", filePath, name)
			t.Register(name, id, NodeTypeFunction)
		}

	case "method_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := string(content[nameNode.StartByte():nameNode.EndByte()])
			// Include receiver type in ID
			receiver := ""
			if recvNode := node.ChildByFieldName("receiver"); recvNode != nil {
				receiver = t.extractReceiverType(recvNode, content)
			}
			fullName := name
			if receiver != "" {
				fullName = fmt.Sprintf("%s.%s", receiver, name)
			}
			id := fmt.Sprintf("%s::%s", filePath, fullName)
			t.Register(fullName, id, NodeTypeMethod)
		}

	case "type_declaration", "type_spec":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := string(content[nameNode.StartByte():nameNode.EndByte()])
			id := fmt.Sprintf("%s::%s", filePath, name)
			t.Register(name, id, NodeTypeClass) // Using Class for types
		}
	}

	// Recurse
	for i := 0; i < int(node.ChildCount()); i++ {
		t.walkAndRegister(node.Child(i), filePath, content)
	}
}

func (t *GoSymbolTable) extractGoImports(node *sitter.Node, content []byte) {
	// Handle import declaration
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)

		switch child.Type() {
		case "import_spec":
			t.extractGoImportSpec(child, content)

		case "import_spec_list":
			// Multiple imports in parens
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "import_spec" {
					t.extractGoImportSpec(spec, content)
				}
			}
		}
	}
}

func (t *GoSymbolTable) extractGoImportSpec(node *sitter.Node, content []byte) {
	var alias, path string

	// Check for explicit alias
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		alias = string(content[nameNode.StartByte():nameNode.EndByte()])
	}

	// Get path
	if pathNode := node.ChildByFieldName("path"); pathNode != nil {
		path = strings.Trim(string(content[pathNode.StartByte():pathNode.EndByte()]), "\"")
	} else {
		// Look for string literal
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "interpreted_string_literal" {
				path = strings.Trim(string(content[child.StartByte():child.EndByte()]), "\"")
				break
			}
		}
	}

	if path != "" {
		// Default alias is the last path component
		if alias == "" {
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}

		// Handle dot imports
		if alias == "." {
			// Symbols are imported directly - would need special handling
			alias = path
		}

		t.RegisterImport(alias, path)
	}
}

func (t *GoSymbolTable) extractReceiverType(node *sitter.Node, content []byte) string {
	// Walk to find the type name in receiver
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "parameter_declaration":
			// Look for type
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				return t.extractTypeName(typeNode, content)
			}
			// Fallback: look for identifier
			for j := 0; j < int(child.ChildCount()); j++ {
				subChild := child.Child(j)
				if subChild.Type() == "type_identifier" || subChild.Type() == "pointer_type" {
					return t.extractTypeName(subChild, content)
				}
			}
		}
	}
	return ""
}

func (t *GoSymbolTable) extractTypeName(node *sitter.Node, content []byte) string {
	switch node.Type() {
	case "type_identifier", "identifier":
		return string(content[node.StartByte():node.EndByte()])
	case "pointer_type":
		// *Type -> Type
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_identifier" {
				return string(content[child.StartByte():child.EndByte()])
			}
		}
	case "generic_type":
		// Get base type name
		if nameNode := node.ChildByFieldName("type"); nameNode != nil {
			return t.extractTypeName(nameNode, content)
		}
	}

	// Fallback: get directory name from filepath for package-level type
	return filepath.Base(string(content[node.StartByte():node.EndByte()]))
}
