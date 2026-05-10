package logger

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const maxEntries = 100

// Logger is a simple rolling-file logger that keeps at most maxEntries lines.
type Logger struct {
	mu   sync.Mutex
	path string
}

// New creates a Logger that writes to path.
func New(path string) *Logger {
	return &Logger{path: path}
}

// Log appends a timestamped message to the log file, trimming to maxEntries.
func (l *Logger) Log(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := fmt.Sprintf("[%s] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))

	// Read existing lines.
	lines := l.readLines()

	// Append new entry.
	lines = append(lines, strings.TrimRight(entry, "\n"))

	// Trim to maxEntries.
	if len(lines) > maxEntries {
		lines = lines[len(lines)-maxEntries:]
	}

	// Write back.
	l.writeLines(lines)
}

func (l *Logger) readLines() []string {
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

func (l *Logger) writeLines(lines []string) {
	f, err := os.Create(l.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: cannot write %s: %v\n", l.path, err)
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	w.Flush()
}
