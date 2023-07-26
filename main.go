package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"

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
		n, _ := sitter.ParseCtx(context.Background(), sourceCode, lang)

		// Query for function definitions
		//functionPattern := `(function_declaration (identifier) @function)`
		functionPattern := `(
(function_declaration
  (
    (identifier) @function.name
    (parameter_list) @param
    (type_identifier)? @return.type)
	)
)`

		// Execute the query
		q, err := sitter.NewQuery([]byte(functionPattern), lang)
		if err != nil {
			panic(err)
		}
		qc := sitter.NewQueryCursor()
		qc.Exec(q, n)

		// Iterate over query results
		for {
			m, ok := qc.NextMatch()
			if !ok {
				break
			}

			for _, c := range m.Captures {
				switch name := q.CaptureNameForId(c.Index); name {
				case "comment":
					fmt.Println("Comment:", string(c.Node.Content(sourceCode)))
				case "function.name":
					fmt.Println("Function:", string(c.Node.Content(sourceCode)))
				case "parameter.name":
					fmt.Println("Parameter:", string(c.Node.Content(sourceCode)))
				case "parameter.type":
					fmt.Println("Type:", string(c.Node.Content(sourceCode)))
				case "param":
					fmt.Println("Param:", string(c.Node.Content(sourceCode)))
				case "return.type":
					fmt.Println("Return Type:", string(c.Node.Content(sourceCode)))
				case "func":
					fmt.Println("Func:", string(c.Node.Content(sourceCode)))
				}
			}

		}
	default:
		panic(ctx.Command())
	}
}
