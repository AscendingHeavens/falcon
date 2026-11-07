package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsFormatted(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"formatted.go", "package a\n\nfunc f() {}\n", true},
		{"windows.go", "package a\r\n\r\nfunc f() {}\r\n", true}, // What git checks out on Windows
		{"unformatted.go", "package a\nfunc   f( ) {}\n", false},
	}
	for _, tt := range tests {
		got, err := isFormatted(write(tt.name, tt.content))
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}

	if _, err := isFormatted(write("broken.go", "package a\nfunc {")); err == nil {
		t.Error("a file that doesn't parse must be an error, not silently unformatted")
	}
}

func TestTotalCoverage(t *testing.T) {
	report := "github.com/a/b/c.go:10:\tF\t100.0%\n" +
		"total:\t\t\t\t(statements)\t\t96.9%\n"

	got, err := totalCoverage(report)
	if err != nil || got != 96.9 {
		t.Errorf("got %v, %v; want 96.9", got, err)
	}

	if _, err := totalCoverage("github.com/a/b/c.go:10:\tF\t100.0%\n"); err == nil {
		t.Error("a report without a total must be an error")
	}
}
