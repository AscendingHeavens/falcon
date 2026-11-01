// Command record runs the benchmarks and saves the output to
// results/benchmark_test_dd-mm-yyyy-hh-mm.txt, showing it live as it runs.
//
// Run it from the bench folder: go run ./cmd/record
//
// Extra arguments are passed on to go test, overriding the defaults:
//
//	go run ./cmd/record -count=1 -bench=JSON
//
// Nothing is saved if a benchmark or one of the checks in bench_test.go fails,
// so every file in results comes from a complete, valid run.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const resultsDir = "results"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "record:", err)
		os.Exit(1)
	}
}

func run(extraArgs []string) error {
	dir, err := moduleDir()
	if err != nil {
		return err
	}

	args := append([]string{"test", "-bench", ".", "-benchmem", "-count=5"}, extraArgs...)
	args = append(args, ".")

	var output bytes.Buffer
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdout = io.MultiWriter(os.Stdout, &output)
	cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("benchmarks failed, nothing saved: %w", err)
	}

	path, err := save(filepath.Join(dir, resultsDir), time.Now(), output.Bytes())
	if err != nil {
		return err
	}
	fmt.Println("saved", path)
	return nil
}

// moduleDir returns the bench module's folder, so that results always end up in the
// same place no matter which subfolder the command is started from.
func moduleDir() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("finding the bench module: %w", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

// save writes data to a file named after t. A second run within the same minute
// replaces the first.
func save(dir string, t time.Time, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "benchmark_test_"+t.Format("02-01-2006-15-04")+".txt")
	return path, os.WriteFile(path, data, 0o644)
}
