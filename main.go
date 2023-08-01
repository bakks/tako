package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/alecthomas/kong"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/protobuf"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"golang.org/x/term"
)

var CLI struct {
	Symbols struct {
		Path string `arg:"" name:"path" help:"Path to search for symbols" type:"path"`
	} `cmd:"" help:"Get symbols from a directory or file"`

	Symbol struct {
		Path    string `arg:"" name:"path" help:"Path to search for symbols" type:"path"`
		Pattern string `arg:"" name:"pattern" help:"Pattern to search for, a regular expression (as parsed by golang regexp library)"`
	} `cmd:"" help:"Print a specific symbol from code files in a given path"`

	Tree struct {
		Path     string `arg:"" name:"file" help:"Path to search for symbols" type:"path"`
		MaxDepth int    `short:"d" default:"10" help:"Maximum depth to print"`
	} `cmd:"" help:"Print the syntax tree for a file"`
}

// comment 1
// comment 2
func testfunc(a int, b bool, c ...string) bool {
	return true
}

type ParsedDocument struct {
	Root         *sitter.Node
	SourceCode   []byte
	Language     *sitter.Language
	LanguageName string
}

func RangeString(rng *sitter.Range) string {
	startPoint := rng.StartPoint
	endPoint := rng.EndPoint
	return fmt.Sprintf("%d:%d-%d:%d", startPoint.Row, startPoint.Column, endPoint.Row, endPoint.Column)
}

func GetRange(node *sitter.Node) *sitter.Range {
	return &sitter.Range{
		StartPoint: node.StartPoint(),
		EndPoint:   node.EndPoint(),
		StartByte:  node.StartByte(),
		EndByte:    node.EndByte(),
	}
}

type Symbol struct {
	Summary string
	Range   *sitter.Range
	Node    *sitter.Node
}

// Sort interface for sorting Symbols by Symbol.Range.StartByte
type SymbolByStartByte []*Symbol

func (a SymbolByStartByte) Len() int           { return len(a) }
func (a SymbolByStartByte) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a SymbolByStartByte) Less(i, j int) bool { return a[i].Range.StartByte < a[j].Range.StartByte }

func (this *Symbol) String() string {
	return fmt.Sprintf("%s %s", this.Summary, RangeString(this.Range))
}

// We probably want a more efficient way to do this
func precedingComments(node *sitter.Node, sourceCode []byte) (string, *sitter.Point, uint32) {
	var comments []string
	var startPoint *sitter.Point
	var startByte uint32

	if node == nil {
		panic("node is nil")
	}
	parent := node.Parent()
	if parent == nil {
		return "", nil, 0
	}

	cursor := sitter.NewTreeCursor(parent)
	cursor.GoToFirstChild()

	// Go through all siblings
	for {
		currNode := cursor.CurrentNode()
		// If the sibling ends after the start of the current node, we break
		if currNode.EndByte() >= node.StartByte() {
			break
		}

		// If the sibling is a comment, we add it to the comments
		if currNode.Type() == "comment" {
			comments = append(comments, string(currNode.Content(sourceCode)))
			if startPoint == nil {
				point := currNode.StartPoint()
				startPoint = &point
				startByte = currNode.StartByte()
			}
		} else {
			startPoint = nil
			comments = []string{}
		}

		// If there are no more siblings, we break
		if !cursor.GoToNextSibling() {
			break
		}
	}

	return strings.Join(comments, "\n"), startPoint, startByte
}

func (this *ParsedDocument) QueryCaptures(queryPattern string, callback func(*sitter.QueryCapture) error) error {
	// Execute the query
	query, err := sitter.NewQuery([]byte(queryPattern), this.Language)
	if err != nil {
		return err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, this.Root)

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}

		for _, cap := range match.Captures {
			if !cap.Node.Parent().Equal(this.Root) {
				continue
			}
			err = callback(&cap)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (this *ParsedDocument) QuerySymbols() ([]*Symbol, error) {
	symbols, err := this.QueryMethods()
	if err != nil {
		return nil, err
	}

	funcs, err := this.QueryFunctions()
	if err != nil {
		return nil, err
	}
	symbols = append(symbols, funcs...)

	typeDefs, err := this.QueryTypeDefinitions()
	if err != nil {
		return nil, err
	}
	symbols = append(symbols, typeDefs...)

	varDecl, err := this.QueryVarDeclarations()
	if err != nil {
		return nil, err
	}
	symbols = append(symbols, varDecl...)

	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].Range.StartByte < symbols[j].Range.StartByte
	})

	return symbols, nil
}

func (this *ParsedDocument) NodeToSymbolWithComments(node *sitter.Node) *Symbol {
	var code strings.Builder
	rng := GetRange(node)

	doc, commentStart, commentStartBytes := precedingComments(node, this.SourceCode)
	if doc != "" {
		code.WriteString(doc)
		code.WriteString("\n")
		rng.StartPoint = *commentStart
		rng.StartByte = commentStartBytes
	}

	code.WriteString(string(node.Content(this.SourceCode)))

	return &Symbol{
		Summary: code.String(),
		Range:   rng,
		Node:    node,
	}
}

// Given a treesitter node, find any preceding comments and stringify all
// of its children except for 'body', returning it as a Symbol.
func (this *ParsedDocument) EverythingExceptBody(node *sitter.Node) *Symbol {
	cursor := sitter.NewTreeCursor(node)
	cursor.GoToFirstChild()

	var code strings.Builder
	rng := GetRange(node)

	doc, commentStart, commentStartBytes := precedingComments(node, this.SourceCode)
	if doc != "" {
		code.WriteString(doc)
		code.WriteString("\n")
		rng.StartPoint = *commentStart
		rng.StartByte = commentStartBytes
	}

	startedWriting := false
	for {
		currNode := cursor.CurrentNode()

		if cursor.CurrentFieldName() != "body" {
			if startedWriting && cursor.CurrentFieldName() != "parameters" {
				code.WriteString(" ")
			}

			code.WriteString(string(currNode.Content(this.SourceCode)))
			startedWriting = true
		}

		if !cursor.GoToNextSibling() {
			break
		}
	}

	symbol := Symbol{
		Summary: code.String(),
		Range:   rng,
		Node:    node,
	}
	return &symbol
}

func (this *ParsedDocument) QueryFunctions() ([]*Symbol, error) {
	// Query for method definitions
	pattern := "(function_declaration) @dec"
	symbols := []*Symbol{}

	this.QueryCaptures(pattern, func(cap *sitter.QueryCapture) error {
		symbol := this.EverythingExceptBody(cap.Node)
		symbols = append(symbols, symbol)
		return nil
	})

	return symbols, nil
}

func (this *ParsedDocument) QueryMethods() ([]*Symbol, error) {
	// Query for method definitions
	pattern := "(method_declaration) @method"
	symbols := []*Symbol{}

	this.QueryCaptures(pattern, func(cap *sitter.QueryCapture) error {
		symbol := this.EverythingExceptBody(cap.Node)
		symbols = append(symbols, symbol)
		return nil
	})

	return symbols, nil
}

func (this *ParsedDocument) QueryTypeDefinitions() ([]*Symbol, error) {
	// Query for method definitions
	pattern := "(type_spec) @type.name"
	symbols := []*Symbol{}

	this.QueryCaptures(pattern, func(cap *sitter.QueryCapture) error {
		symbol := this.EverythingExceptBody(cap.Node)
		symbols = append(symbols, symbol)
		return nil
	})

	return symbols, nil
}

func (this *ParsedDocument) QueryVarDeclarations() ([]*Symbol, error) {
	// Query for method definitions
	pattern := "(var_declaration) @dec"
	symbols := []*Symbol{}

	this.QueryCaptures(pattern, func(cap *sitter.QueryCapture) error {
		symbol := this.EverythingExceptBody(cap.Node)
		symbols = append(symbols, symbol)
		return nil
	})

	return symbols, nil
}

func (this *ParsedDocument) SymbolName(node *sitter.Node) string {
	fieldNames := []string{"name"}

	for _, fieldName := range fieldNames {
		childNode := node.ChildByFieldName(fieldName)
		if childNode != nil {
			return string(childNode.Content(this.SourceCode))
		}
	}

	if this.LanguageName == "go" {
		switch node.Type() {
		case "var_declaration", "type_declaration":
			code := string(node.Child(1).Content(this.SourceCode))
			return strings.Split(code, " ")[0]
		}
	}

	return ""
}

func (this *ParsedDocument) FindSymbolsMatching(regex *regexp.Regexp) ([]*Symbol, error) {
	cursor := sitter.NewTreeCursor(this.Root)
	cursor.GoToFirstChild()
	matches := []*Symbol{}

	for {
		currNode := cursor.CurrentNode()
		name := this.SymbolName(currNode)

		if name != "" && regex.MatchString(name) {
			match := this.NodeToSymbolWithComments(currNode)
			matches = append(matches, match)
		}

		if !cursor.GoToNextSibling() {
			break
		}
	}

	return matches, nil
}

func NewParsedDocument(sourceCode []byte, language *sitter.Language, langName string) (*ParsedDocument, error) {
	rootNode, err := sitter.ParseCtx(context.Background(), sourceCode, language)
	if err != nil {
		return nil, err
	}

	return &ParsedDocument{
		Root:         rootNode,
		SourceCode:   sourceCode,
		Language:     language,
		LanguageName: langName,
	}, nil
}

var codeSuffixes = []string{
	"go", "rs", "js", "ts", "c", "h", "cpp", "cxx", "cc", "hpp", "hxx",
	"hh", "java", "php", "py", "rb", "cs", "scala", "proto"}

var ignores = []string{
	"vendor", "node_modules", "third_party", "build", "dist", "out", "target",
	"bin", ".git"}

func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func GetLanguageFromExtension(ext string) (*sitter.Language, string) {
	if ext[0] == '.' {
		ext = ext[1:]
	}

	switch ext {
	case "go":
		return golang.GetLanguage(), "go"
	case "rs":
		return rust.GetLanguage(), "rust"
	case "js":
		return javascript.GetLanguage(), "javascript"
	case "ts":
		return typescript.GetLanguage(), "typescript"
	case "c", "h":
		return c.GetLanguage(), "c"
	case "cpp", "cxx", "cc", "hpp", "hxx", "hh":
		return cpp.GetLanguage(), "cpp"
	case "java":
		return java.GetLanguage(), "java"
	case "php":
		return php.GetLanguage(), "php"
	case "py":
		return python.GetLanguage(), "python"
	case "rb":
		return ruby.GetLanguage(), "ruby"
	case "cs":
		return csharp.GetLanguage(), "csharp"
	case "scala":
		return scala.GetLanguage(), "scala"
	case "proto":
		return protobuf.GetLanguage(), "protobuf"
	default:
		return nil, ""
	}
}

func PrintFileSymbols(path string) error {
	doc, err := ParseFile(path)
	if err != nil {
		return err
	}

	sym, err := doc.QuerySymbols()
	if err != nil {
		return err
	}

	fmt.Printf("%s:\n", path)

	for _, s := range sym {
		fmt.Printf("%s\n\n", s.String())
	}

	return nil
}

func ParseFile(path string) (*ParsedDocument, error) {
	// Read the file
	sourceCode, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// get extension
	ext := filepath.Ext(path)
	// Parse source code
	lang, langName := GetLanguageFromExtension(ext)
	if lang == nil {
		return nil, fmt.Errorf("Unsupported file extension: %s", ext)
	}

	return NewParsedDocument(sourceCode, lang, langName)
}

func PrintFileSymbolsMatching(path string, regex *regexp.Regexp) error {
	doc, err := ParseFile(path)
	if err != nil {
		return err
	}

	sym, err := doc.FindSymbolsMatching(regex)
	if err != nil {
		return err
	}

	if len(sym) == 0 {
		return nil
	}

	fmt.Printf("%s:\n", path)

	for _, s := range sym {
		fmt.Printf("%s\n%s\n\n", RangeString(s.Range), s.Summary)
	}

	return nil
}

func CodeFileWalker(path string, callback func(string) error) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return err
	}

	if !fileInfo.IsDir() {
		return callback(path)
	}

	err = filepath.Walk(path, func(subPath string, info os.FileInfo, err error) error {
		if err != nil || subPath == path {
			return err
		}

		// Check if the file has a code extension
		fileSuffix := filepath.Ext(subPath)
		if len(fileSuffix) > 0 && fileSuffix[0] == '.' {
			fileSuffix = fileSuffix[1:]
		}

		// last segment of path
		_, last := filepath.Split(subPath)
		if sliceContains(ignores, last) {
			return filepath.SkipDir
		}

		if !info.IsDir() && sliceContains(codeSuffixes, fileSuffix) {
			return callback(subPath)
		}

		if info.IsDir() {
			CodeFileWalker(subPath, callback)
		}

		return nil
	})

	return err
}

// Given a path, recurse through all files, check if they are code files,
// and if so, parse them and print out all symbols with PrintFileSymbols
func PrintSymbols(path string) error {
	return CodeFileWalker(path, func(subPath string) error {
		return PrintFileSymbols(subPath)
	})
}

// foo bar
func PrintSymbolsMatching(path string, pattern string) error {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("Search pattern could not be parsed as a regular expression: %s", err)
	}

	return CodeFileWalker(path, func(subPath string) error {
		return PrintFileSymbolsMatching(subPath, regex)
	})
}

func isOnlyWhitespace(s string) bool {
	for _, c := range s {
		if !unicode.IsSpace(c) {
			return false
		}
	}
	return true
}

// constants for printing parse tree
const (
	LINE_NONE = iota
	LINE_VERTICAL
	LINE_CHILD
	LINE_LAST_CHILD
)

var ttyWidth = -1

const tree_padding = 15

// get terminal width
func getTermWidth() int {
	if ttyWidth != -1 {
		return ttyWidth
	}

	width, _, err := term.GetSize(0)
	if err != nil {
		return 80
	}
	ttyWidth = width
	return width
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// replace chunks of whitespace with a single space
func collapseWhitespace(s string) string {
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(s, " ")
}

func (this *ParsedDocument) PrintParseTree(cursor *sitter.TreeCursor, depth int, depthRemaining int, childLines []int) {
	if depthRemaining == 0 {
		return
	}

	var treeStructure string
	if depth >= 1 {
		for _, line := range childLines {
			switch line {
			case LINE_NONE:
				treeStructure += "  "
			case LINE_VERTICAL:
				treeStructure += "│ "
			case LINE_CHILD:
				treeStructure += "├ "
			case LINE_LAST_CHILD:
				treeStructure += "╰ "
			}
		}
	}

	node := cursor.CurrentNode()
	var nodeString string
	if node.IsNamed() {
		nodeString = fmt.Sprintf("%s %s", node.Type(), cursor.CurrentFieldName())
	} else if !isOnlyWhitespace(node.Type()) {
		nodeString = fmt.Sprintf("\"%s\"", node.Type())
	}

	if nodeString != "" {
		nodeCode := string(node.Content(this.SourceCode))
		// replace whitespace with space
		nodeCode = collapseWhitespace(nodeCode)

		str := fmt.Sprintf("%s%s", treeStructure, nodeString)
		remaining := getTermWidth() - len(str) - tree_padding
		// fill up the remaining space with the nodevalue string
		if remaining > 0 {
			str += strings.Repeat(" ", tree_padding)
			str += nodeCode[:min(len(nodeCode), remaining)]
		}
		fmt.Println(str)
	}

	childCount := int(node.ChildCount())
	if cursor.GoToFirstChild() {
		children := 0
		newChildLines := append(childLines, 1)

		if len(newChildLines) > 1 {
			lastLine := len(newChildLines) - 2
			if newChildLines[lastLine] == 3 {
				newChildLines[lastLine] = LINE_NONE
			} else if newChildLines[lastLine] == 2 {
				newChildLines[lastLine] = LINE_VERTICAL
			}
		}

		for cursor.GoToNextSibling() {
			if children == childCount-2 {
				newChildLines[len(newChildLines)-1] = LINE_LAST_CHILD
			} else {
				newChildLines[len(newChildLines)-1] = LINE_CHILD
			}

			this.PrintParseTree(cursor, depth+1, depthRemaining-1, newChildLines)
			children++
		}
		cursor.GoToParent()
	}
}

func PrintTree(path string, maxDepth int) error {
	// Read the file
	sourceCode, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	// get extension
	ext := filepath.Ext(path)
	// Parse source code
	lang, langName := GetLanguageFromExtension(ext)
	if lang == nil {
		return fmt.Errorf("Unsupported file extension: %s", ext)
	}

	doc, err := NewParsedDocument(sourceCode, lang, langName)
	if err != nil {
		return err
	}

	cursor := sitter.NewTreeCursor(doc.Root)
	doc.PrintParseTree(cursor, 0, maxDepth, []int{})
	return nil
}

// tako main function
// executes from CLI
func main() {
	ctx := kong.Parse(&CLI)
	var err error

	switch ctx.Command() {
	case "symbols <path>":
		err = PrintSymbols(CLI.Symbols.Path)

	case "symbol <path> <pattern>":
		err = PrintSymbolsMatching(CLI.Symbol.Path, CLI.Symbol.Pattern)

	case "tree <file>":
		err = PrintTree(CLI.Tree.Path, CLI.Tree.MaxDepth)

	default:
		panic(ctx.Command())
	}

	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
}
