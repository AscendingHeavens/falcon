package main

import (
	"encoding/json"
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"unicode"
)

type DocEntry struct {
	Name    string     `json:"name"`
	Doc     string     `json:"doc"`
	Methods []DocEntry `json:"methods,omitempty"`
	Package string     `json:"package,omitempty"` // subpackage name
}

type Docs struct {
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

func parsePackage(path string, fsSet *token.FileSet) (*Docs, error) {
	pkgs, err := parser.ParseDir(fsSet, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var docs Docs
	for pkgName, pkg := range pkgs {
		p := doc.New(pkg, path, 0)

		// Types
		for _, t := range p.Types {
			if !isExported(t.Name) {
				continue
			}
			entry := DocEntry{
				Name:    t.Name,
				Doc:     t.Doc,
				Package: pkgName,
			}

			for _, m := range t.Methods {
				if !isExported(m.Name) || m.Doc == "" {
					continue
				}
				entry.Methods = append(entry.Methods, DocEntry{
					Name: m.Name,
					Doc:  m.Doc,
				})
			}
			docs.Types = append(docs.Types, entry)
		}

		// Functions
		for _, f := range p.Funcs {
			if !isExported(f.Name) || f.Doc == "" || len(f.Name) >= 4 && f.Name[:4] == "Test" {
				continue
			}
			docs.Functions = append(docs.Functions, DocEntry{
				Name:    f.Name,
				Doc:     f.Doc,
				Package: pkgName,
			})
		}
	}

	return &docs, nil
}

func main() {
	fsSet := token.NewFileSet()
	finalDocs := Docs{}

	// Walk all subdirectories
	err := filepath.WalkDir("./", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		// Skip hidden directories like .git
		if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "example" || d.Name() == "docs" {
			return filepath.SkipDir
		}

		pkgDocs, err := parsePackage(path, fsSet)
		if err != nil {
			// Not a Go package? skip silently
			return nil
		}

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

	fmt.Println("Full docs.json generated with all subpackages!")
}
