// Package queue implements an async file-based work queue.
//
// The webhook handler drops a small JSON file into queueDir for each sync
// request.  A background goroutine scans that directory every 5 seconds,
// exclusively locks each pending file, and passes the task to the provided
// handler function.  Files for the same project are deduplicated: only the
// most-recently written one is kept in the directory at any time.
package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const pollInterval = 5 * time.Second

// Task describes one pending sync operation.
type Task struct {
	Project   string `json:"project"`
	GiteaRepo string `json:"gitea_repo"`
	Ref       string `json:"ref"`
	After     string `json:"after"`
}

// Handler is called for each dequeued Task.
type Handler func(t Task) error

// Queue is an async file-based work queue.
type Queue struct {
	dir     string
	handler Handler
}

// New creates a Queue that stores pending tasks in dir.
func New(dir string, handler Handler) (*Queue, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating queue dir %s: %w", dir, err)
	}
	return &Queue{dir: dir, handler: handler}, nil
}

// Enqueue writes a task file for project into the queue directory.
// If a file for the same project already exists it is overwritten so that
// only the latest push event is processed (deduplication).
func (q *Queue) Enqueue(t Task) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	name := sanitizeFilename(t.Project) + ".pending"
	path := filepath.Join(q.dir, name)
	return os.WriteFile(path, data, 0o644)
}

// Run starts the polling loop.  It blocks until done is closed.
func (q *Queue) Run(done <-chan struct{}) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			q.processPending()
		}
	}
}

func (q *Queue) processPending() {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "queue: reading dir %s: %v\n", q.dir, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pending") {
			continue
		}
		q.processFile(filepath.Join(q.dir, e.Name()))
	}
}

func (q *Queue) processFile(path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "queue: open %s: %v\n", path, err)
		}
		return
	}
	defer f.Close()

	// Try to acquire an exclusive lock (non-blocking).
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Another process/goroutine holds the lock – skip.
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Read the task.
	var task Task
	if err := json.NewDecoder(f).Decode(&task); err != nil {
		fmt.Fprintf(os.Stderr, "queue: decode %s: %v\n", path, err)
		os.Remove(path) //nolint:errcheck
		return
	}

	// Execute the sync handler.
	if err := q.handler(task); err != nil {
		fmt.Fprintf(os.Stderr, "queue: handler error for %s: %v\n", task.Project, err)
		// Leave the file in place so it will be retried on the next poll.
		return
	}

	// Remove the file after successful processing.
	os.Remove(path) //nolint:errcheck
}

// sanitizeFilename replaces characters that are unsafe in filenames with '_'.
func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' ||
			r == '"' || r == '<' || r == '>' || r == '|' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
