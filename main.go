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
	StartPoint    *sitter.Point
	Node          *sitter.Node
}

func (f *ParsedFunction) String() string {
	var str strings.Builder
	if f.Documentation != "" {
		str.WriteString(f.Documentation)
		str.WriteString("\n")
	}
	str.WriteString(fmt.Sprintf("%s%s %s", f.Name, f.Params, f.ReturnType))

	startPoint := f.StartPoint
	endPoint := f.Node.EndPoint()
	str.WriteString(fmt.Sprintf(" %d:%d-%d:%d", startPoint.Row, startPoint.Column, endPoint.Row, endPoint.Column))
	return str.String()
}

type ParsedSymbol struct {
	Function ParsedFunction
}

func (s *ParsedSymbol) String() string {
	return fmt.Sprintf("%s", s.Function.String())
}

// We probably want a more efficient way to do this
func precedingComments(node *sitter.Node, sourceCode []byte) (string, *sitter.Point) {
	var comments []string
	var startPoint *sitter.Point

	cursor := sitter.NewTreeCursor(node.Parent())
	cursor.GoToFirstChild()

	// Go through all siblings
	for {
		currNode := cursor.CurrentNode()
		// If the sibling ends after the start of the current node, we break
		if currNode.EndByte() >= node.StartByte() {
			break
		}

		//log.Println(cursor.CurrentNode().Type())
		// If the sibling is a comment, we add it to the comments
		if currNode.Type() == "comment" {
			comments = append(comments, string(currNode.Content(sourceCode)))
			if startPoint == nil {
				point := cursor.CurrentNode().StartPoint()
				startPoint = &point
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

	return strings.Join(comments, "\n"), startPoint
}

// Identify function declarations
func Functions(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedFunction, error) {
	// Query for function definitions
	functionPattern := `(function_declaration
	(identifier) @function.name)
`

	// Execute the query
	query, err := sitter.NewQuery([]byte(functionPattern), lang)
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
				startPoint := node.StartPoint()
				newFunc := ParsedFunction{
					Name:       string(cap.Node.Content(sourceCode)),
					Node:       node,
					StartPoint: &startPoint,
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
				doc, commentStart := precedingComments(node, sourceCode)
				if doc != "" {
					newFunc.Documentation = doc
					newFunc.StartPoint = commentStart
				}
				funcs = append(funcs, &newFunc)
			}
		}
	}

	return funcs, nil
}

func Symbols(rootNode *sitter.Node, lang *sitter.Language, sourceCode []byte) ([]*ParsedSymbol, error) {
	funcs, err := Functions(rootNode, lang, sourceCode)
	if err != nil {
		return nil, err
	}

	symbols := []*ParsedSymbol{}

	for _, f := range funcs {
		symbol := ParsedSymbol{
			Function: *f,
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

		symbols, err := Symbols(rootNode, lang, sourceCode)
		if err != nil {
			log.Fatal(err)
		}

		for _, s := range symbols {
			fmt.Println(s)
		}

	default:
		panic(ctx.Command())
	}
}
