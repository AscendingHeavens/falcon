package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

type DocEntry struct {
	Name     string     `json:"name"`
	Doc      string     `json:"doc"`
	Methods  []DocEntry `json:"methods,omitempty"`
	Package  string     `json:"package,omitempty"`
	Code     string     `json:"code,omitempty"`     // <-- code snippet
	Examples []Example  `json:"examples,omitempty"` // From Example functions in the package's tests
}

// Example is a testable example. go test runs it and checks its output, so it can't go stale.
type Example struct {
	Name   string `json:"name,omitempty"` // The suffix of ExampleFoo_suffix, if any
	Code   string `json:"code"`
	Output string `json:"output,omitempty"`
}

type Docs struct {
	Packages  []DocEntry `json:"packages"` // Each package's overview, from its package comment
	Types     []DocEntry `json:"types"`
	Functions []DocEntry `json:"functions"`
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

// gofmt prints code the way gofmt does: tabs to indent, spaces to align, so that
// comments still line up once a website shows the tabs as spaces.
var gofmt = &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}

// convert an ast.Decl to Go code
func snippetFromDecl(fs *token.FileSet, decl ast.Decl) string {
	var buf bytes.Buffer
	if err := gofmt.Fprint(&buf, fs, decl); err != nil {
		return ""
	}
	return buf.String()
}

// examples converts go/doc's examples into the code a reader sees: the body of the
// Example function, without the braces around it or the "// Output:" comment, since the
// output is shown separately.
func examples(fs *token.FileSet, list []*doc.Example) []Example {
	var out []Example
	for _, ex := range list {
		var buf bytes.Buffer
		if err := gofmt.Fprint(&buf, fs, &printer.CommentedNode{Node: ex.Code, Comments: ex.Comments}); err != nil {
			continue
		}

		code := strings.TrimSpace(buf.String())
		code = strings.TrimSuffix(strings.TrimPrefix(code, "{"), "}")

		var lines []string
		for _, line := range strings.Split(strings.Trim(code, "\n"), "\n") {
			line = strings.TrimPrefix(line, "\t")
			if strings.HasPrefix(line, "// Output:") || strings.HasPrefix(line, "// Unordered output:") {
				break
			}
			lines = append(lines, line)
		}

		code = strings.TrimRight(strings.Join(lines, "\n"), "\n")
		out = append(out, Example{Name: ex.Suffix, Code: code, Output: ex.Output})
	}
	return out
}

// parsePackage documents the package in path. Its files are the ones the go command would
// build, so build tags are respected. Test files only provide examples.
func parsePackage(path string, fsSet *token.FileSet) (*Docs, error) {
	pkg, err := build.ImportDir(path, 0)
	if err != nil {
		return nil, err
	}

	names := append(append(slices.Clone(pkg.GoFiles), pkg.TestGoFiles...), pkg.XTestGoFiles...)
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		file, err := parser.ParseFile(fsSet, filepath.Join(path, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	p, err := doc.NewFromFiles(fsSet, files, path)
	if err != nil {
		return nil, err
	}

	var docs Docs
	if p.Doc != "" {
		docs.Packages = append(docs.Packages, DocEntry{Name: pkg.Name, Doc: p.Doc, Examples: examples(fsSet, p.Examples)})
	}

	// Types
	for _, t := range p.Types {
		if !isExported(t.Name) {
			continue
		}
		entry := DocEntry{
			Name:     t.Name,
			Doc:      t.Doc,
			Package:  pkg.Name,
			Code:     snippetFromDecl(fsSet, t.Decl),
			Examples: examples(fsSet, t.Examples),
		}

		for _, m := range t.Methods {
			if !isExported(m.Name) || m.Doc == "" {
				continue
			}
			entry.Methods = append(entry.Methods, DocEntry{
				Name:     m.Name,
				Doc:      m.Doc,
				Code:     snippetFromDecl(fsSet, m.Decl),
				Examples: examples(fsSet, m.Examples),
			})
		}
		docs.Types = append(docs.Types, entry)
	}

	// Functions. go/doc files a function that returns one of the package's types under
	// that type, such as New under Server, so those are collected from the types too.
	funcs := p.Funcs
	for _, t := range p.Types {
		funcs = append(funcs, t.Funcs...)
	}
	for _, f := range funcs {
		if !isExported(f.Name) || f.Doc == "" || (len(f.Name) >= 4 && f.Name[:4] == "Test") {
			continue
		}
		docs.Functions = append(docs.Functions, DocEntry{
			Name:     f.Name,
			Doc:      f.Doc,
			Package:  pkg.Name,
			Code:     snippetFromDecl(fsSet, f.Decl),
			Examples: examples(fsSet, f.Examples),
		})
	}

	return &docs, nil
}

func main() {
	fsSet := token.NewFileSet()
	finalDocs := Docs{}

	err := filepath.WalkDir("./", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		// Skip everything that isn't part of Falcon's API
		switch d.Name() {
		case ".git", "node_modules", "example", "docs", "bench", "tools", "testdata":
			return filepath.SkipDir
		}

		pkgDocs, err := parsePackage(path, fsSet)
		if err != nil {
			return nil
		}

		finalDocs.Packages = append(finalDocs.Packages, pkgDocs.Packages...)
		finalDocs.Types = append(finalDocs.Types, pkgDocs.Types...)
		finalDocs.Functions = append(finalDocs.Functions, pkgDocs.Functions...)
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	out, err := json.MarshalIndent(finalDocs, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("docs.json", out, 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Full docs.json generated with all subpackages and code snippets!")
}
