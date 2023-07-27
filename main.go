package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/alecthomas/kong"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

var CLI struct {
	File string `arg:"" name:"file" help:"Go file to parse." type:"path"`
}

// comment 1
// comment 2
func testfunc(a int, b bool, c ...string) bool {
	return true
}

type ParsedFunction struct {
	Name          string
	Params        string
	ReturnType    string
	Documentation string
	Range         *sitter.Range
	Node          *sitter.Node
}

func (this *ParsedFunction) String() string {
	var str strings.Builder
	if this.Documentation != "" {
		str.WriteString(this.Documentation)
		str.WriteString("\n")
	}
	str.WriteString(fmt.Sprintf("%s%s %s", this.Name, this.Params, this.ReturnType))
	str.WriteString(" ")
	str.WriteString(RangeString(this.Range))
	return str.String()
}

type ParsedMethod struct {
	Name          string
	Params        string
	ReturnType    string
	Documentation string
	Range         *sitter.Range
	Node          *sitter.Node
}

func (this *ParsedMethod) Testxxx(a int, b bool, c ...string) (bool, error) {
	return true, nil
}

func (this *ParsedMethod) String() string {
	var str strings.Builder
	if this.Documentation != "" {
		str.WriteString(this.Documentation)
		str.WriteString("\n")
	}
	str.WriteString(fmt.Sprintf("%s%s %s", this.Name, this.Params, this.ReturnType))
	str.WriteString(" ")
	str.WriteString(RangeString(this.Range))
	return str.String()
}

type ParsedTypeDefinition struct {
	Definition    string
	Documentation string
	Range         *sitter.Range
	Node          *sitter.Node
}

func (this *ParsedTypeDefinition) String() string {
	var str strings.Builder
	if this.Documentation != "" {
		str.WriteString(this.Documentation)
		str.WriteString("\n")
	}
	str.WriteString(this.Definition)
	str.WriteString(" ")
	str.WriteString(RangeString(this.Range))
	return str.String()
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

type ParsedSymbol struct {
	Function       *ParsedFunction
	TypeDefinition *ParsedTypeDefinition
	Method         *ParsedMethod
}

func (this *ParsedSymbol) String() string {
	if this.TypeDefinition != nil {
		return this.TypeDefinition.String()
	} else if this.Function != nil {
		return this.Function.String()
	} else if this.Method != nil {
		return this.Method.String()
	}
	return ""
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

// Identify function declarations
func QueryFunctions(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedFunction, error) {
	// Query for function definitions
	pattern := "(function_declaration	(identifier) @function.name)"

	// Execute the query
	query, err := sitter.NewQuery([]byte(pattern), lang)
	if err != nil {
		return nil, err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, rootNode)

	funcs := []*ParsedFunction{}

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}

		for _, cap := range match.Captures {
			switch name := query.CaptureNameForId(cap.Index); name {
			case "function.name":
				node := cap.Node.Parent()
				rng := GetRange(node)

				newFunc := ParsedFunction{
					Name:  string(cap.Node.Content(sourceCode)),
					Node:  node,
					Range: rng,
				}

				paramList := node.ChildByFieldName("parameters")
				if paramList != nil {
					newFunc.Params = string(paramList.Content(sourceCode))
				} else {
					newFunc.Params = "()"
				}
				returnType := node.ChildByFieldName("result")
				if returnType != nil {
					newFunc.ReturnType = string(returnType.Content(sourceCode))
				}
				doc, commentStart, commentStartBytes := precedingComments(node, sourceCode)
				if doc != "" {
					newFunc.Documentation = doc
					newFunc.Range.StartPoint = *commentStart
					newFunc.Range.StartByte = commentStartBytes
				}
				funcs = append(funcs, &newFunc)
			}
		}
	}

	return funcs, nil
}

func QueryMethods2(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedMethod, error) {
	// Query for method definitions
	pattern := `(method_declaration
		(parameter_list) @receiver
		(field_identifier) @method.name
		(parameter_list) @method.parameters
		(type_identifier) @method.type)`

	// Execute the query
	query, err := sitter.NewQuery([]byte(pattern), lang)
	if err != nil {
		return nil, err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, rootNode)

	methods := []*ParsedMethod{}

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}
		fmt.Printf("match: %v\n", match)

		for _, cap := range match.Captures {
			fmt.Printf("cap: %s\n", string(cap.Node.Content(sourceCode)))
		}
	}

	return methods, nil
}

func QueryMethods3(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedMethod, error) {
	// Query for method definitions
	pattern := `(method_declaration) @method`

	// Execute the query
	query, err := sitter.NewQuery([]byte(pattern), lang)
	if err != nil {
		return nil, err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, rootNode)

	methods := []*ParsedMethod{}

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}
		fmt.Printf("match: %v\n", match)

		for _, cap := range match.Captures {
			// iterate over capture children
			cursor := sitter.NewTreeCursor(cap.Node)
			cursor.GoToFirstChild()

			code := ""

			for {
				currNode := cursor.CurrentNode()
				//fmt.Printf("child: %s %s\n", currNode.Type(), cursor.CurrentFieldName())
				if cursor.CurrentFieldName() != "body" {
					fmt.Printf("child: %s %s %s\n", currNode.Type(), cursor.CurrentFieldName(), string(currNode.Content(sourceCode)))
					code += string(currNode.Content(sourceCode))
				}

				ok = cursor.GoToNextSibling()
				if !ok {
					break
				}
			}

			fmt.Printf("code: %s\n", code)
		}
	}

	return methods, nil
}

// Identify method declarations
func QueryMethods(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedMethod, error) {
	// Query for method definitions
	pattern := "(method_declaration) @method.name"
	// (method_declaration
	//   name: (identifier) @method.name
	//   type: (function_type
	//     parameters: (field_list
	//       (field_declaration
	//         name: (identifier) @parameter.name
	//         type: (_) @parameter.type))))

	// Execute the query
	query, err := sitter.NewQuery([]byte(pattern), lang)
	if err != nil {
		return nil, err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, rootNode)

	methods := []*ParsedMethod{}

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}

		for _, cap := range match.Captures {
			fmt.Printf("cap: %v\n", cap)
			switch name := query.CaptureNameForId(cap.Index); name {
			case "method.name":
				node := cap.Node.Parent()
				rng := GetRange(node)

				newMethod := ParsedMethod{
					Name:  string(cap.Node.Content(sourceCode)),
					Node:  node,
					Range: rng,
				}
				log.Printf("newMethod: %v\n", newMethod)

				paramList := node.ChildByFieldName("parameters")
				log.Printf("paramList: %v\n", paramList)
				if paramList != nil {
					newMethod.Params = string(paramList.Content(sourceCode))
				} else {
					newMethod.Params = "()"
				}
				returnType := node.ChildByFieldName("result")
				log.Printf("returnType: %v\n", returnType)
				if returnType != nil {
					newMethod.ReturnType = string(returnType.Content(sourceCode))
				}
				doc, commentStart, commentStartBytes := precedingComments(node, sourceCode)
				if doc != "" {
					newMethod.Documentation = doc
					newMethod.Range.StartPoint = *commentStart
					newMethod.Range.StartByte = commentStartBytes
				}
				methods = append(methods, &newMethod)
			}
		}
	}

	return methods, nil
}

func QueryTypeDefinitions(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedTypeDefinition, error) {
	// Query for type definitions
	pattern := "(type_spec (type_identifier)) @type.name"

	// Execute the query
	query, err := sitter.NewQuery([]byte(pattern), lang)
	if err != nil {
		return nil, err
	}
	queryCursor := sitter.NewQueryCursor()
	queryCursor.Exec(query, rootNode)

	types := []*ParsedTypeDefinition{}

	// Iterate over query results
	for {
		match, ok := queryCursor.NextMatch()
		if !ok {
			break
		}

		for _, cap := range match.Captures {
			switch name := query.CaptureNameForId(cap.Index); name {
			case "type.name":
				node := cap.Node.Parent()
				rng := GetRange(node)

				newType := ParsedTypeDefinition{
					Definition: string(cap.Node.Content(sourceCode)),
					Node:       node,
					Range:      rng,
				}

				doc, commentStart, commentStartBytes := precedingComments(node, sourceCode)
				if doc != "" {
					newType.Documentation = doc
					newType.Range.StartPoint = *commentStart
					newType.Range.StartByte = commentStartBytes
				}
				types = append(types, &newType)
			}
		}
	}

	return types, nil
}

func QuerySymbols(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedSymbol, error) {
	funcs, err := QueryFunctions(rootNode, lang, sourceCode)
	if err != nil {
		return nil, err
	}

	symbols := []*ParsedSymbol{}

	for _, f := range funcs {
		symbol := ParsedSymbol{
			Function: f,
		}
		symbols = append(symbols, &symbol)
	}

	types, err := QueryTypeDefinitions(rootNode, lang, sourceCode)
	if err != nil {
		return nil, err
	}

	for _, t := range types {
		symbol := ParsedSymbol{
			TypeDefinition: t,
		}
		symbols = append(symbols, &symbol)
	}

	methods, err := QueryMethods3(rootNode, lang, sourceCode)
	if err != nil {
		return nil, err
	}

	for _, method := range methods {
		symbol := ParsedSymbol{
			Method: method,
		}
		symbols = append(symbols, &symbol)
	}

	return symbols, nil
}

// tako main function
// executes from CLI
func main() {
	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "<file>":
		// Read the file
		sourceCode, err := ioutil.ReadFile(CLI.File)
		if err != nil {
			log.Fatal(err)
		}
		// Parse source code
		lang := golang.GetLanguage()
		rootNode, _ := sitter.ParseCtx(context.Background(), sourceCode, lang)

		symbols, err := QuerySymbols(rootNode, lang, sourceCode)
		if err != nil {
			log.Fatal(err)
		}

		for _, symbol := range symbols {
			fmt.Printf("%s\n\n", symbol.String())
		}

	default:
		panic(ctx.Command())
	}
}
