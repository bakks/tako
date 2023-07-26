# Tako

This is a command line tool (written in golang) that describes the interfaces of a code file, for example producing a list of functions. The purpose is to succintly summarize the contents of the file which can be externally exercised.

Example:

```bash
> tako foo.go
function bar(a int, b string) bool
  This function counts the number of vowels in string b and returns true if it is greater than a.
```

Tako uses Tree-sitter golang bindings to parse the file and queries the parse tree to discover the useful symbols.
