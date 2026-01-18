package parser

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/heefoo/codeloom/internal/parser/grammars/clojure_lang"
	"github.com/heefoo/codeloom/internal/parser/grammars/commonlisp_lang"
	"github.com/heefoo/codeloom/internal/parser/grammars/julia_lang"
	"github.com/heefoo/codeloom/internal/util"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

type Language string

const (
	LangC          Language = "c"
	LangCPP        Language = "cpp"
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangJavaScript Language = "javascript"
	LangTypeScript Language = "typescript"
	LangRust       Language = "rust"
	LangJava       Language = "java"
	LangClojure    Language = "clojure"
	LangJulia      Language = "julia"
	LangCommonLisp Language = "commonlisp"
)

type NodeType string

const (
	NodeTypeFunction  NodeType = "function"
	NodeTypeClass     NodeType = "class"
	NodeTypeMethod    NodeType = "method"
	NodeTypeStruct    NodeType = "struct"
	NodeTypeInterface NodeType = "interface"
	NodeTypeEnum      NodeType = "enum"
	NodeTypeVariable  NodeType = "variable"
	NodeTypeImport    NodeType = "import"
	NodeTypeType      NodeType = "type"
	NodeTypeMacro     NodeType = "macro"
	NodeTypeModule    NodeType = "module"
)

type EdgeType string

const (
	EdgeTypeCalls      EdgeType = "calls"
	EdgeTypeImports    EdgeType = "imports"
	EdgeTypeUses       EdgeType = "uses"
	EdgeTypeExtends    EdgeType = "extends"
	EdgeTypeImplements EdgeType = "implements"
)

type CodeNode struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	NodeType    NodeType          `json:"node_type"`
	Language    Language          `json:"language"`
	FilePath    string            `json:"file_path"`
	StartLine   int               `json:"start_line"`
	EndLine     int               `json:"end_line"`
	StartCol    int               `json:"start_col"`
	EndCol      int               `json:"end_col"`
	Content     string            `json:"content"`
	Signature   string            `json:"signature,omitempty"`
	DocComment  string            `json:"doc_comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"` // @semantic fields
}

type CodeEdge struct {
	FromID   string   `json:"from_id"`
	ToID     string   `json:"to_id"`
	EdgeType EdgeType `json:"edge_type"`
}

type ParseResult struct {
	Nodes      []CodeNode
	Edges      []CodeEdge
	FilesTotal int // Total files found (supported languages)
}

type Parser struct {
	languages         map[Language]*sitter.Language
	mu                sync.RWMutex
	enableSymbolTable bool // Opt-in for Go, Clojure, C symbol resolution
	extractEdges      bool // Enable edge extraction
}

// ParserOption configures the parser
type ParserOption func(*Parser)

// WithSymbolTable enables symbol table-based resolution for Go, Clojure, C
func WithSymbolTable() ParserOption {
	return func(p *Parser) {
		p.enableSymbolTable = true
	}
}

// WithEdgeExtraction enables call graph edge extraction
func WithEdgeExtraction() ParserOption {
	return func(p *Parser) {
		p.extractEdges = true
	}
}

func NewParser(opts ...ParserOption) *Parser {
	p := &Parser{
		languages:    make(map[Language]*sitter.Language),
		extractEdges: true, // Enable by default
	}

	for _, opt := range opts {
		opt(p)
	}

	// Register built-in languages
	p.languages[LangC] = c.GetLanguage()
	p.languages[LangCPP] = cpp.GetLanguage()
	p.languages[LangGo] = golang.GetLanguage()
	p.languages[LangPython] = python.GetLanguage()
	p.languages[LangJavaScript] = javascript.GetLanguage()
	p.languages[LangTypeScript] = typescript.GetLanguage()
	p.languages[LangRust] = rust.GetLanguage()
	p.languages[LangJava] = java.GetLanguage()

	// Register custom grammars (Lisp family + Julia)
	p.languages[LangClojure] = clojure_lang.GetLanguage()
	p.languages[LangJulia] = julia_lang.GetLanguage()
	p.languages[LangCommonLisp] = commonlisp_lang.GetLanguage()

	return p
}

func (p *Parser) GetLanguage(lang Language) *sitter.Language {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.languages[lang]
}

func (p *Parser) DetectLanguage(filename string) Language {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".c", ".h":
		return LangC
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return LangCPP
	case ".go":
		return LangGo
	case ".py":
		return LangPython
	case ".js", ".mjs", ".cjs":
		return LangJavaScript
	case ".ts", ".tsx":
		return LangTypeScript
	case ".rs":
		return LangRust
	case ".java":
		return LangJava
	case ".clj", ".cljs", ".cljc", ".edn":
		return LangClojure
	case ".jl":
		return LangJulia
	case ".lisp", ".lsp", ".cl", ".asd", ".asdf":
		return LangCommonLisp
	default:
		return ""
	}
}

// IsSupportedFile returns true if the file extension is supported
func (p *Parser) IsSupportedFile(filePath string) bool {
	return p.DetectLanguage(filePath) != ""
}

func (p *Parser) ParseFile(ctx context.Context, filePath string) (*ParseResult, error) {
	lang := p.DetectLanguage(filePath)
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type: %s", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return p.ParseContent(ctx, filePath, lang, content)
}

func (p *Parser) ParseContent(ctx context.Context, filePath string, lang Language, content []byte) (*ParseResult, error) {
	language := p.GetLanguage(lang)
	if language == nil {
		return nil, fmt.Errorf("language not supported: %s", lang)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(language)
	defer parser.Close()

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}
	defer tree.Close()

	result := &ParseResult{
		Nodes: []CodeNode{},
		Edges: []CodeEdge{},
	}

	// Extract nodes based on language
	rootNode := tree.RootNode()
	p.extractNodes(rootNode, filePath, lang, content, result)

	// Extract edges if enabled
	if p.extractEdges {
		p.extractEdgesForFile(rootNode, filePath, lang, content, result)
	}

	return result, nil
}

// lineRange represents a line range for a function
type lineRange struct {
	start int
	end   int
	id    string
}

// extractEdgesForFile extracts call edges from the AST
func (p *Parser) extractEdgesForFile(root *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	// Build a sorted list of function line ranges for O(log n) lookup
	var funcRanges []lineRange
	for _, node := range result.Nodes {
		if node.NodeType == NodeTypeFunction || node.NodeType == NodeTypeMethod {
			funcRanges = append(funcRanges, lineRange{
				start: node.StartLine,
				end:   node.EndLine,
				id:    node.ID,
			})
		}
	}

	// Create appropriate extractor based on language and symbol table setting
	var symbolTable SymbolTable

	if p.enableSymbolTable {
		switch lang {
		case LangGo:
			goSymTable := NewGoSymbolTable()
			goSymTable.BuildFromAST(root, filePath, content)
			symbolTable = goSymTable
		case LangClojure:
			cljSymTable := NewClojureSymbolTable()
			cljSymTable.BuildFromAST(root, filePath, content)
			symbolTable = cljSymTable
		case LangC, LangCPP:
			cSymTable := NewCSymbolTable()
			cSymTable.BuildFromAST(root, filePath, content)
			symbolTable = cSymTable
		}
	}

	// Single-pass edge extraction with function context lookup
	if lang == LangClojure {
		var cljSymTable *ClojureSymbolTable
		if symbolTable != nil {
			cljSymTable = symbolTable.(*ClojureSymbolTable)
		}
		extractor := NewClojureEdgeExtractor(cljSymTable)
		p.extractEdgesSinglePass(root, filePath, content, result, funcRanges, extractor)
		return
	}

	if lang == LangC || lang == LangCPP {
		var cSymTable *CSymbolTable
		if symbolTable != nil {
			cSymTable = symbolTable.(*CSymbolTable)
		}
		extractor := NewCEdgeExtractor(cSymTable)
		p.extractEdgesSinglePass(root, filePath, content, result, funcRanges, extractor)
		return
	}

	// Generic extractor
	extractor := NewEdgeExtractor(symbolTable)
	p.extractEdgesSinglePass(root, filePath, content, result, funcRanges, extractor)
}

// edgeExtractorInterface defines the interface for edge extractors
type edgeExtractorInterface interface {
	ExtractEdges(node *sitter.Node, callerID string, filePath string, content []byte, result *ParseResult)
}

// extractEdgesSinglePass walks the AST once, extracting edges with function context
func (p *Parser) extractEdgesSinglePass(node *sitter.Node, filePath string, content []byte, result *ParseResult, funcRanges []lineRange, extractor edgeExtractorInterface) {
	if node == nil {
		return
	}

	line := int(node.StartPoint().Row) + 1

	// Find which function contains this line
	callerID := ""
	for _, fr := range funcRanges {
		if line >= fr.start && line <= fr.end {
			callerID = fr.id
			break
		}
	}

	// Check for call expressions and delegate to extractor
	nodeType := node.Type()
	switch nodeType {
	case "call_expression", "method_invocation", "invocation_expression":
		if callerID != "" {
			// Create a minimal result to collect edges from this call
			tempResult := &ParseResult{Edges: []CodeEdge{}}
			extractor.ExtractEdges(node, callerID, filePath, content, tempResult)
			result.Edges = append(result.Edges, tempResult.Edges...)
			// Don't recurse into children - extractor already handled it
			return
		}
	case "list_lit":
		// Clojure function calls - let the Clojure extractor handle
		if callerID != "" {
			tempResult := &ParseResult{Edges: []CodeEdge{}}
			extractor.ExtractEdges(node, callerID, filePath, content, tempResult)
			result.Edges = append(result.Edges, tempResult.Edges...)
			return
		}
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		p.extractEdgesSinglePass(node.Child(i), filePath, content, result, funcRanges, extractor)
	}
}

func (p *Parser) extractNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	nodeType := node.Type()

	// Extract based on node type
	switch lang {
	case LangGo:
		p.extractGoNodes(node, filePath, lang, content, result)
	case LangPython:
		p.extractPythonNodes(node, filePath, lang, content, result)
	case LangC, LangCPP:
		p.extractCNodes(node, filePath, lang, content, result)
	case LangJavaScript, LangTypeScript:
		p.extractJSNodes(node, filePath, lang, content, result)
	case LangRust:
		p.extractRustNodes(node, filePath, lang, content, result)
	case LangJava:
		p.extractJavaNodes(node, filePath, lang, content, result)
	case LangClojure:
		p.extractClojureNodes(node, filePath, lang, content, result)
	case LangJulia:
		p.extractJuliaNodes(node, filePath, lang, content, result)
	case LangCommonLisp:
		p.extractCommonLispNodes(node, filePath, lang, content, result)
	default:
		// Generic extraction
		p.extractGenericNodes(node, filePath, lang, content, result)
	}

	// Recursively process children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		p.extractNodes(child, filePath, lang, content, result)
	}

	_ = nodeType // silence unused warning
}

func (p *Parser) extractGoNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			codeNode := CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				StartCol:    int(node.StartPoint().Column),
				EndCol:      int(node.EndPoint().Column),
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			}
			result.Nodes = append(result.Nodes, codeNode)
		}

	case "method_declaration":
		name := p.getChildByField(node, "name", content)
		receiver := p.getChildByField(node, "receiver", content)
		if name != "" {
			fullName := name
			if receiver != "" {
				fullName = fmt.Sprintf("%s.%s", receiver, name)
			}
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, fullName),
				Name:        fullName,
				NodeType:    NodeTypeMethod,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "type_declaration":
		// Check for struct or interface
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "type_spec" {
				typeName := p.getChildByField(child, "name", content)
				typeNode := child.ChildByFieldName("type")
				if typeNode != nil && typeName != "" {
					var nt NodeType
					switch typeNode.Type() {
					case "struct_type":
						nt = NodeTypeStruct
					case "interface_type":
						nt = NodeTypeInterface
					default:
						nt = NodeTypeType
					}
					result.Nodes = append(result.Nodes, CodeNode{
						ID:          fmt.Sprintf("%s::%s", filePath, typeName),
						Name:        typeName,
						NodeType:    nt,
						Language:    lang,
						FilePath:    filePath,
						StartLine:   int(node.StartPoint().Row) + 1,
						EndLine:     int(node.EndPoint().Row) + 1,
						Content:     string(content[node.StartByte():node.EndByte()]),
						DocComment:  p.extractDocComment(node, content),
						Annotations: p.extractAnnotations(node, content),
					})
				}
			}
		}

	case "import_declaration":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::import_%d", filePath, node.StartPoint().Row),
			Name:      "import",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractPythonNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "class_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeClass,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "import_statement", "import_from_statement":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::import_%d", filePath, node.StartPoint().Row),
			Name:      "import",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractCNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_definition":
		// Get declarator which contains the function name
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			name := p.extractFunctionName(declarator, content)
			if name != "" {
				result.Nodes = append(result.Nodes, CodeNode{
					ID:          fmt.Sprintf("%s::%s", filePath, name),
					Name:        name,
					NodeType:    NodeTypeFunction,
					Language:    lang,
					FilePath:    filePath,
					StartLine:   int(node.StartPoint().Row) + 1,
					EndLine:     int(node.EndPoint().Row) + 1,
					Content:     string(content[node.StartByte():node.EndByte()]),
					DocComment:  p.extractDocComment(node, content),
					Annotations: p.extractAnnotations(node, content),
				})
			}
		}

	case "struct_specifier":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeStruct,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "enum_specifier":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeEnum,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "preproc_include":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::include_%d", filePath, node.StartPoint().Row),
			Name:      "include",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractJSNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_declaration", "function":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "class_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeClass,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "method_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeMethod,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "import_statement":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::import_%d", filePath, node.StartPoint().Row),
			Name:      "import",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractRustNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_item":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "struct_item":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeStruct,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "enum_item":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeEnum,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "trait_item":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeInterface,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "use_declaration":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::use_%d", filePath, node.StartPoint().Row),
			Name:      "use",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractJavaNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "method_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeMethod,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "class_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeClass,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "interface_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeInterface,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "enum_declaration":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeEnum,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "import_declaration":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::import_%d", filePath, node.StartPoint().Row),
			Name:      "import",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) extractClojureNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	// Clojure uses list_lit with sym_lit children where the first symbol determines the form type
	if node.Type() != "list_lit" {
		return
	}

	// Get the first symbol to determine the form type
	formType := ""
	formName := ""
	symIndex := 0

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "sym_lit" {
			symName := p.getClojureSymbolText(child, content)
			if symIndex == 0 {
				formType = symName
			} else if symIndex == 1 {
				formName = symName
			}
			symIndex++
			if symIndex > 1 {
				break
			}
		}
	}

	if formName == "" {
		return
	}

	switch formType {
	case "defn", "defn-":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:          fmt.Sprintf("%s::%s", filePath, formName),
			Name:        formName,
			NodeType:    NodeTypeFunction,
			Language:    lang,
			FilePath:    filePath,
			StartLine:   int(node.StartPoint().Row) + 1,
			EndLine:     int(node.EndPoint().Row) + 1,
			Content:     string(content[node.StartByte():node.EndByte()]),
			DocComment:  p.getClojureDocstring(node, content),
			Annotations: p.extractAnnotations(node, content),
		})

	case "defmacro":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:          fmt.Sprintf("%s::macro_%s", filePath, formName),
			Name:        formName,
			NodeType:    NodeTypeFunction,
			Language:    lang,
			FilePath:    filePath,
			StartLine:   int(node.StartPoint().Row) + 1,
			EndLine:     int(node.EndPoint().Row) + 1,
			Content:     string(content[node.StartByte():node.EndByte()]),
			Signature:   "macro",
			DocComment:  p.getClojureDocstring(node, content),
			Annotations: p.extractAnnotations(node, content),
		})

	case "defprotocol":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:          fmt.Sprintf("%s::%s", filePath, formName),
			Name:        formName,
			NodeType:    NodeTypeInterface,
			Language:    lang,
			FilePath:    filePath,
			StartLine:   int(node.StartPoint().Row) + 1,
			EndLine:     int(node.EndPoint().Row) + 1,
			Content:     string(content[node.StartByte():node.EndByte()]),
			DocComment:  p.getClojureDocstring(node, content),
			Annotations: p.extractAnnotations(node, content),
		})

	case "defrecord", "deftype":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:          fmt.Sprintf("%s::%s", filePath, formName),
			Name:        formName,
			NodeType:    NodeTypeStruct,
			Language:    lang,
			FilePath:    filePath,
			StartLine:   int(node.StartPoint().Row) + 1,
			EndLine:     int(node.EndPoint().Row) + 1,
			Content:     string(content[node.StartByte():node.EndByte()]),
			DocComment:  p.getClojureDocstring(node, content),
			Annotations: p.extractAnnotations(node, content),
		})

	case "ns":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::ns_%s", filePath, formName),
			Name:      formName,
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

// getClojureSymbolText extracts the symbol name from a sym_lit node
func (p *Parser) getClojureSymbolText(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "sym_name" {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

// getClojureDocstring extracts a docstring from a Clojure form
func (p *Parser) getClojureDocstring(node *sitter.Node, content []byte) string {
	// Check for preceding comment
	if node.PrevSibling() != nil {
		prev := node.PrevSibling()
		if isCommentNode(prev.Type()) {
			return cleanComment(string(content[prev.StartByte():prev.EndByte()]))
		}
	}

	// Check for inline docstring (third element if it's a string)
	strIndex := 0
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "str_lit" && strIndex == 0 {
			// First string after defn name and params could be a docstring
			text := string(content[child.StartByte():child.EndByte()])
			return cleanDocstring(text)
		}
		if child.Type() == "sym_lit" {
			strIndex++ // Count symbols to skip form name and function name
		}
	}
	return ""
}

func (p *Parser) extractJuliaNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "function_definition", "short_function_definition":
		name := p.getChildByField(node, "name", content)
		if name == "" {
			// Try to get identifier from signature → call_expression → identifier
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type() == "signature" {
					// Look inside signature for call_expression
					for j := 0; j < int(child.ChildCount()); j++ {
						sigChild := child.Child(j)
						if sigChild.Type() == "call_expression" || sigChild.Type() == "identifier" {
							name = p.extractJuliaFunctionName(sigChild, content)
							if name != "" {
								break
							}
						}
					}
				} else if child.Type() == "identifier" || child.Type() == "call_expression" {
					name = p.extractJuliaFunctionName(child, content)
				}
				if name != "" {
					break
				}
			}
		}
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "struct_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeStruct,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "abstract_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeInterface,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "module_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::module_%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeImport,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "import_statement", "using_statement":
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::import_%d", filePath, node.StartPoint().Row),
			Name:      "import",
			NodeType:  NodeTypeImport,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})

	case "macro_definition":
		name := p.getChildByField(node, "name", content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::macro_%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				Signature:   "macro",
				DocComment:  p.extractDocComment(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}
	}
}

func (p *Parser) extractCommonLispNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	switch node.Type() {
	case "defun_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defmacro_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::macro_%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeFunction,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				Signature:   "macro",
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defclass_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeClass,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defstruct_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeStruct,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defgeneric_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::%s", filePath, name),
				Name:        name,
				NodeType:    NodeTypeInterface,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defmethod_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:          fmt.Sprintf("%s::method_%s_%d", filePath, name, node.StartPoint().Row),
				Name:        name,
				NodeType:    NodeTypeMethod,
				Language:    lang,
				FilePath:    filePath,
				StartLine:   int(node.StartPoint().Row) + 1,
				EndLine:     int(node.EndPoint().Row) + 1,
				Content:     string(content[node.StartByte():node.EndByte()]),
				DocComment:  p.getLispDocstring(node, content),
				Annotations: p.extractAnnotations(node, content),
			})
		}

	case "defpackage_form", "in_package_form":
		name := p.getLispSymbolName(node, content)
		if name != "" {
			result.Nodes = append(result.Nodes, CodeNode{
				ID:        fmt.Sprintf("%s::package_%s", filePath, name),
				Name:      name,
				NodeType:  NodeTypeImport,
				Language:  lang,
				FilePath:  filePath,
				StartLine: int(node.StartPoint().Row) + 1,
				EndLine:   int(node.EndPoint().Row) + 1,
				Content:   string(content[node.StartByte():node.EndByte()]),
			})
		}
	}
}

// extractDocComment finds the doc comment preceding a node
func (p *Parser) extractDocComment(node *sitter.Node, content []byte) string {
	// Look for comment siblings before this node
	if node.PrevSibling() != nil {
		prev := node.PrevSibling()
		if isCommentNode(prev.Type()) {
			return cleanComment(string(content[prev.StartByte():prev.EndByte()]))
		}
	}

	// For Python functions/classes, docstrings are inside the body block
	// Structure: function_definition -> block -> expression_statement -> string
	if node.Type() == "function_definition" || node.Type() == "class_definition" {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "block" && child.ChildCount() > 0 {
				// Look for first expression_statement containing a string
				for j := 0; j < int(child.ChildCount()); j++ {
					stmt := child.Child(j)
					if stmt.Type() == "expression_statement" && stmt.ChildCount() > 0 {
						expr := stmt.Child(0)
						if expr.Type() == "string" {
							text := string(content[expr.StartByte():expr.EndByte()])
							return cleanDocstring(text)
						}
					}
					// Only check the first non-comment statement
					if stmt.Type() != "comment" && stmt.Type() != "expression_statement" {
						break
					}
					// If it's an expression_statement but not a string docstring, stop
					if stmt.Type() == "expression_statement" {
						if stmt.ChildCount() > 0 && stmt.Child(0).Type() != "string" {
							break
						}
					}
				}
				break
			}
		}
	}

	// For other languages, check first child (e.g., string literals)
	if node.ChildCount() > 0 {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "string" || child.Type() == "string_literal" || child.Type() == "comment" {
				text := string(content[child.StartByte():child.EndByte()])
				// Check if it looks like a docstring (first expression in body)
				if strings.HasPrefix(text, `"""`) || strings.HasPrefix(text, `"`) {
					return cleanDocstring(text)
				}
			}
			// Stop after we've passed potential docstring location
			if child.Type() != "string" && child.Type() != "string_literal" && child.Type() != "comment" {
				break
			}
		}
	}

	return ""
}

// extractAnnotations parses @semantic or @annotation comment blocks
func (p *Parser) extractAnnotations(node *sitter.Node, content []byte) map[string]string {
	annotations := make(map[string]string)

	// Look for annotation comment before node
	if node.PrevSibling() != nil {
		prev := node.PrevSibling()
		if isCommentNode(prev.Type()) {
			text := string(content[prev.StartByte():prev.EndByte()])
			if strings.Contains(text, "@semantic") || strings.Contains(text, "@annotation") {
				parseAnnotationBlock(text, annotations)
			}
		}
	}

	// Also check within the node content for embedded annotations
	nodeContent := string(content[node.StartByte():node.EndByte()])
	if strings.Contains(nodeContent, "@semantic") || strings.Contains(nodeContent, "@annotation") {
		parseAnnotationBlock(nodeContent, annotations)
	}

	return annotations
}

// parseAnnotationBlock extracts key-value pairs from annotation comments
func parseAnnotationBlock(text string, annotations map[string]string) {
	lines := strings.Split(text, "\n")
	var currentKey string
	var currentValue strings.Builder

	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimPrefix(line, "*/")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, ";")
		line = strings.TrimSpace(line)

		// Check for key: value pattern
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			// Only accept known annotation keys
			if isAnnotationKey(key) {
				// Save previous key if exists
				if currentKey != "" {
					annotations[currentKey] = strings.TrimSpace(currentValue.String())
				}
				currentKey = key
				currentValue.Reset()
				currentValue.WriteString(strings.TrimSpace(line[idx+1:]))
				continue
			}
		}

		// Continuation of previous value (for multi-line values like lists)
		if currentKey != "" && line != "" && !strings.HasPrefix(line, "@") {
			if currentValue.Len() > 0 {
				currentValue.WriteString(" ")
			}
			currentValue.WriteString(line)
		}
	}

	// Save last key
	if currentKey != "" {
		annotations[currentKey] = strings.TrimSpace(currentValue.String())
	}
}

// isAnnotationKey checks if a key is a recognized annotation field
func isAnnotationKey(key string) bool {
	knownKeys := map[string]bool{
		"id": true, "kind": true, "name": true, "summary": true,
		"responsibility": true, "inputs": true, "outputs": true,
		"side_effects": true, "calls": true, "called_by": true,
		"data_reads": true, "data_writes": true, "lifetime": true,
		"invariants": true, "error_handling": true, "thread_safety": true,
		"related_symbols": true, "tags": true, "description": true,
		"returns": true, "params": true, "throws": true, "see": true,
		"since": true, "deprecated": true, "author": true, "version": true,
	}
	return knownKeys[strings.ToLower(key)]
}

// isCommentNode checks if a node type represents a comment
func isCommentNode(nodeType string) bool {
	return strings.Contains(nodeType, "comment") ||
		nodeType == "line_comment" ||
		nodeType == "block_comment" ||
		nodeType == "documentation_comment"
}

// cleanComment removes comment syntax from a comment string
func cleanComment(comment string) string {
	comment = strings.TrimSpace(comment)

	// Block comments /* */
	comment = strings.TrimPrefix(comment, "/*")
	comment = strings.TrimSuffix(comment, "*/")

	// Doc comments /** */
	comment = strings.TrimPrefix(comment, "/**")

	// Triple slash ///
	lines := strings.Split(comment, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "///")
		line = strings.TrimPrefix(line, "//!")
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, ";")
		line = strings.TrimPrefix(line, ";;")
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, " ")
}

// cleanDocstring removes docstring delimiters
func cleanDocstring(docstring string) string {
	docstring = strings.TrimSpace(docstring)
	docstring = strings.Trim(docstring, `"`)
	docstring = strings.TrimPrefix(docstring, `""`)
	docstring = strings.TrimSuffix(docstring, `""`)
	return strings.TrimSpace(docstring)
}

// getLispDocstring extracts docstring from Lisp forms
func (p *Parser) getLispDocstring(node *sitter.Node, content []byte) string {
	// In Lisp: (defun name "docstring" ...)
	// Docstring is typically the second or third element
	foundName := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		nodeType := child.Type()

		// Skip past the name
		if nodeType == "symbol" || nodeType == "sym_lit" || nodeType == "identifier" {
			foundName = true
			continue
		}

		// After name, look for string
		if foundName && (nodeType == "string" || nodeType == "str_lit") {
			text := string(content[child.StartByte():child.EndByte()])
			return cleanDocstring(text)
		}

		// If we hit a list/vector after name, no docstring
		if foundName && (nodeType == "list" || nodeType == "vector" || nodeType == "list_lit" || nodeType == "vec_lit") {
			break
		}
	}
	return ""
}

// getLispSymbolName extracts the symbol name from a Lisp form
func (p *Parser) getLispSymbolName(node *sitter.Node, content []byte) string {
	// In Lisp, the name is typically the second element of the list
	// (defun NAME ...)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		nodeType := child.Type()
		if nodeType == "symbol" || nodeType == "sym_lit" || nodeType == "identifier" {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

// extractJuliaFunctionName extracts function name from Julia AST
func (p *Parser) extractJuliaFunctionName(node *sitter.Node, content []byte) string {
	switch node.Type() {
	case "identifier":
		return string(content[node.StartByte():node.EndByte()])
	case "call_expression":
		// function foo(x) style
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "identifier" {
				return string(content[child.StartByte():child.EndByte()])
			}
		}
	}
	return ""
}

func (p *Parser) extractGenericNodes(node *sitter.Node, filePath string, lang Language, content []byte, result *ParseResult) {
	// Generic extraction for unknown languages
	nodeType := node.Type()
	if strings.Contains(nodeType, "function") || strings.Contains(nodeType, "method") {
		name := p.getChildByField(node, "name", content)
		if name == "" {
			name = fmt.Sprintf("anonymous_%d", node.StartPoint().Row)
		}
		result.Nodes = append(result.Nodes, CodeNode{
			ID:        fmt.Sprintf("%s::%s", filePath, name),
			Name:      name,
			NodeType:  NodeTypeFunction,
			Language:  lang,
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			Content:   string(content[node.StartByte():node.EndByte()]),
		})
	}
}

func (p *Parser) getChildByField(node *sitter.Node, field string, content []byte) string {
	child := node.ChildByFieldName(field)
	if child != nil {
		return string(content[child.StartByte():child.EndByte()])
	}
	return ""
}

func (p *Parser) extractFunctionName(node *sitter.Node, content []byte) string {
	// For C/C++, function name can be nested in declarators
	switch node.Type() {
	case "identifier":
		return string(content[node.StartByte():node.EndByte()])
	case "function_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return p.extractFunctionName(declarator, content)
		}
	case "pointer_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return p.extractFunctionName(declarator, content)
		}
	}

	// Try first child as fallback
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" {
			return string(content[child.StartByte():child.EndByte()])
		}
		if name := p.extractFunctionName(child, content); name != "" {
			return name
		}
	}

	return ""
}

// shouldExclude checks if a path should be excluded based on patterns
// Matches against directory name and also checks if any path component matches
func shouldExclude(path string, name string, excludePatterns []string) bool {
	for _, pattern := range excludePatterns {
		// Direct match against directory/file name
		if util.MatchPattern(pattern, name) {
			return true
		}
		// Also check if pattern appears as a path component
		// This handles cases like "node_modules" appearing anywhere in the path
		pathParts := strings.Split(filepath.ToSlash(path), "/")
		for _, part := range pathParts {
			if util.MatchPattern(pattern, part) {
				return true
			}
		}
	}
	return false
}

// ParseDirectory parses all supported files in a directory
func (p *Parser) ParseDirectory(ctx context.Context, dir string, excludePatterns []string) (*ParseResult, error) {
	result := &ParseResult{
		Nodes:      []CodeNode{},
		Edges:      []CodeEdge{},
		FilesTotal: 0,
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			// Check exclude patterns against directory name and path
			if shouldExclude(path, d.Name(), excludePatterns) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check exclude patterns for files too (e.g., *.min.js)
		if shouldExclude(path, d.Name(), excludePatterns) {
			return nil
		}

		// Check if file is supported
		lang := p.DetectLanguage(path)
		if lang == "" {
			return nil
		}

		// Count this as a file to parse
		result.FilesTotal++

		// Parse file
		fileResult, err := p.ParseFile(ctx, path)
		if err != nil {
			// Log error but continue with other files
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", path, err)
			return nil
		}

		result.Nodes = append(result.Nodes, fileResult.Nodes...)
		result.Edges = append(result.Edges, fileResult.Edges...)

		return nil
	})

	return result, err
}
