# Tako

Tako is a command line tool to parse specific objects within code files, for
example to print a list of functions or find a symbol matching a search pattern.
I developed it because I was tired of copying/pasting code from an editor
into a large language model, and it's helpful to reduce the code to the important
pieces (e.g. function declarations) rather than wasting the context window on
everything else (like the function body).

The tool is written in Golang and uses [Tree-Sitter](https://tree-sitter.github.io)
to parse code. Tree-Sitter is relatively language agnostic, so I've compiled
in parsers for many common languages but only tested rigorously with Go so far.

Here are language parsers included in the Tako command:

-   c
-   cpp
-   csharp
-   golang
-   java
-   javascript
-   php
-   protobuf
-   python
-   ruby
-   rust
-   scala
-   typescript

## Examples

Find a symbol with a name matching a regex.

```bash
> tako symbol main.go '^ParsedDocument$'
/Users/bakks/tako/main.go:
53:0-58:1
type ParsedDocument struct {
        Root         *sitter.Node
        SourceCode   []byte
        Language     *sitter.Language
        LanguageName string
}
```

Succinctly print the important symbols found in code, omitting function/method
bodies and providing the lines and columns of that symbol.

```bash
> tako symbols .
/Users/bakks/tako/main.go:
// comment 1
// comment 2
func testfunc(a int, b bool, c ...string) bool 47:0-51:1

func RangeString(rng *sitter.Range) string 60:0-64:1
...
```

Print out the Tree-sitter parse tree for a specific code file.

```bash
> tako tree main.go --depth 4
source_file                package main import ( "context" "fmt" "io/ioutil" "os" "path/fi
├ import_declaration                import ( "context" "fmt" "io/ioutil" "os" "path/file
│ ╰ import_spec_list                ( "context" "fmt" "io/ioutil" "os" "path/filepath"
│   ├ import_spec                "context"
│   ├ import_spec                "fmt"
│   ├ import_spec                "io/ioutil"
│   ├ import_spec                "os"
│   ╰ ")"               )
├ var_declaration                var CLI struct { Symbols struct { Path string `arg:"" n
│ ╰ var_spec                CLI struct { Symbols struct { Path string `arg:"" name:"pa
│   ╰ struct_type type               struct { Symbols struct { Path string `arg:"" nam
├ comment                // comment 1
├ comment                // comment 2
├ function_declaration                func testfunc(a int, b bool, c ...string) bool { r
│ ├ identifier name               testfunc
│ ├ parameter_list parameters               (a int, b bool, c ...string)
...
```

## Command Help

```bash
> tako --help
Tako uses Tree-sitter golang bindings to parse the file and queries the parse tree to discover the useful symbols.
Usage: tako <command>

Flags:
  -h, --help    Show context-sensitive help.

Commands:
  symbols <path>
    Get symbols from a directory or file

  symbol <path> <pattern>
    Print a specific symbol from code files in a given path

  tree <file>
    Print the syntax tree for a file

Run "tako <command> --help" for more information on a command.
```
