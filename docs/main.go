package main

import (
	"encoding/json"
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"log"
	"os"
	"unicode"
)

type DocEntry struct {
	Name    string     `json:"name"`
	Doc     string     `json:"doc"`
	Methods []DocEntry `json:"methods,omitempty"`
}

type Docs struct {
	Types     []DocEntry `json:"types"`
	Functions []DocEntry `json:"functions"`
}

func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func main() {
	dir := "./" // path to your package
	fs := token.NewFileSet()

	pkgs, err := parser.ParseDir(fs, dir, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	var docs Docs

	for _, pkg := range pkgs {
		p := doc.New(pkg, "./", 0)

		// Types
		for _, t := range p.Types {
			if !isExported(t.Name) {
				continue
			}

			entry := DocEntry{
				Name: t.Name,
				Doc:  t.Doc,
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
			if !isExported(f.Name) || f.Doc == "" || f.Name[:4] == "Test" {
				continue
			}
			docs.Functions = append(docs.Functions, DocEntry{
				Name: f.Name,
				Doc:  f.Doc,
			})
		}
	}

	out, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("docs.json", out, 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Println("docs.json generated!")
}
