package logger_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathan77886/gitea2github/internal/logger"
)

func TestLogRolling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	l := logger.New(path)

	// Write 110 entries – the log must be capped at 100.
	for i := 0; i < 110; i++ {
		l.Log("entry %d", i)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening log: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	if len(lines) != 100 {
		t.Errorf("expected 100 log lines, got %d", len(lines))
	}

	// The first kept line must be entry 10 (entries 0-9 were trimmed).
	if !strings.Contains(lines[0], "entry 10") {
		t.Errorf("first line should contain 'entry 10', got: %s", lines[0])
	}
	// The last kept line must be entry 109.
	if !strings.Contains(lines[99], "entry 109") {
		t.Errorf("last line should contain 'entry 109', got: %s", lines[99])
	}
}

func TestLogCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "create.log")

	l := logger.New(path)
	l.Log("hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("expected 'hello world' in log, got: %s", data)
	}
}

func TestLogMaxEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max.log")

	l := logger.New(path)

	// Write exactly 100 entries.
	for i := 0; i < 100; i++ {
		l.Log("msg-%d", i)
	}

	lines := countLines(t, path)
	if lines != 100 {
		t.Errorf("expected 100 lines, got %d", lines)
	}

	// Write one more – still 100.
	l.Log("extra")
	lines = countLines(t, path)
	if lines != 100 {
		t.Errorf("expected 100 lines after capping, got %d", lines)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
	}
	return n
}
