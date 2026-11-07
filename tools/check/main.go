// Command check runs the repository checks that need more than a single command. It's
// written in Go rather than shell so that the Makefile works the same on Windows, macOS
// and Linux.
//
// Usage, from the tools folder (the Makefile does this for you):
//
//	go run ./check -root .. fmt
//	go run ./check -root .. cover -threshold 85
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	root := flag.String("root", ".", "the repository root")
	flag.Parse()

	var err error
	switch flag.Arg(0) {
	case "fmt":
		err = checkFormat(*root)
	case "cover":
		coverFlags := flag.NewFlagSet("cover", flag.ExitOnError)
		threshold := coverFlags.Float64("threshold", 85, "minimum total coverage, in percent")
		_ = coverFlags.Parse(flag.Args()[1:]) // ExitOnError exits by itself
		err = checkCoverage(*root, *threshold)
	default:
		err = errors.New("usage: check [-root dir] fmt | cover [-threshold percent]")
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// checkFormat fails if any Go file isn't formatted the way gofmt would format it. Windows
// line endings are ignored, because git may add them on checkout, and gofmt -l would
// otherwise report every file as unformatted there.
func checkFormat(root string) error {
	var unformatted []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		formatted, err := isFormatted(path)
		if err != nil {
			return err
		}
		if !formatted {
			rel, _ := filepath.Rel(root, path)
			unformatted = append(unformatted, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(unformatted) > 0 {
		return fmt.Errorf("not formatted, run make fmt:\n  %s", strings.Join(unformatted, "\n  "))
	}
	fmt.Println("All Go files are formatted.")
	return nil
}

// isFormatted reports whether a Go file is already formatted, ignoring line endings.
func isFormatted(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))

	formatted, err := format.Source(content)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return bytes.Equal(content, formatted), nil
}

// checkCoverage runs the tests CI runs and fails if total coverage is below threshold.
// The example app and the docs generator are left out, as in CI, because they have no tests.
func checkCoverage(root string, threshold float64) error {
	listed, err := output(root, "go", "list", "./...")
	if err != nil {
		return err
	}
	var packages []string
	for _, pkg := range strings.Fields(listed) {
		if !strings.Contains(pkg, "/example") && !strings.Contains(pkg, "/docs") {
			packages = append(packages, pkg)
		}
	}

	profile, err := os.CreateTemp("", "falcon-coverage-*.out")
	if err != nil {
		return err
	}
	_ = profile.Close() // go test writes to it by name
	defer func() { _ = os.Remove(profile.Name()) }()

	args := append([]string{"test", "-count=1", "-covermode=atomic", "-coverprofile=" + profile.Name()}, packages...)
	test := exec.Command("go", args...)
	test.Dir = root
	test.Stdout = os.Stdout
	test.Stderr = os.Stderr
	if err := test.Run(); err != nil {
		return fmt.Errorf("tests failed: %w", err)
	}

	report, err := output(root, "go", "tool", "cover", "-func="+profile.Name())
	if err != nil {
		return err
	}
	total, err := totalCoverage(report)
	if err != nil {
		return err
	}

	fmt.Printf("Total coverage: %.1f%%\n", total)
	if total < threshold {
		return fmt.Errorf("coverage %.1f%% is below %.1f%%", total, threshold)
	}
	return nil
}

// totalCoverage reads the percentage from the "total:" line of go tool cover -func.
func totalCoverage(report string) (float64, error) {
	for _, line := range strings.Split(report, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "total:" {
			return strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-1], "%"), 64)
		}
	}
	return 0, errors.New("no total in the coverage report")
}

// output runs a command in dir and returns what it printed.
func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}
