package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSave_NamesFileAfterTheTime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "results") // doesn't exist yet
	when := time.Date(2026, time.November, 3, 9, 5, 0, 0, time.Local)

	path, err := save(dir, when, []byte("data"))

	assert.NoError(t, err)
	assert.Equal(t, "benchmark_test_03-11-2026-09-05.txt", filepath.Base(path))
	got, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, "data", string(got))
}

func TestSave_SameMinuteReplacesTheEarlierResult(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, time.November, 3, 21, 45, 0, 0, time.Local)

	first, err := save(dir, when, []byte("a longer first result"))
	assert.NoError(t, err)
	second, err := save(dir, when, []byte("second"))
	assert.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, "benchmark_test_03-11-2026-21-45.txt", filepath.Base(second))

	got, err := os.ReadFile(second)
	assert.NoError(t, err)
	assert.Equal(t, "second", string(got))

	files, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Len(t, files, 1)
}
