package queue_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nathan77886/gitea2github/internal/queue"
)

func TestEnqueueCreatesFile(t *testing.T) {
	dir := t.TempDir()

	var called int32
	q, err := queue.New(dir, func(task queue.Task) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("creating queue: %v", err)
	}

	task := queue.Task{Project: "my-project", GiteaRepo: "https://gitea.example.com/owner/repo.git", Ref: "refs/heads/main", After: "abc123"}
	if err := q.Enqueue(task); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Verify the file exists and has correct content.
	name := "my-project.pending"
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading queue file: %v", err)
	}
	var got queue.Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Project != task.Project || got.After != task.After {
		t.Errorf("task mismatch: got %+v, want %+v", got, task)
	}
}

func TestQueueDeduplication(t *testing.T) {
	dir := t.TempDir()

	q, err := queue.New(dir, func(task queue.Task) error { return nil })
	if err != nil {
		t.Fatalf("creating queue: %v", err)
	}

	// Enqueue the same project twice – only one file should exist.
	q.Enqueue(queue.Task{Project: "proj", After: "aaa"}) //nolint:errcheck
	q.Enqueue(queue.Task{Project: "proj", After: "bbb"}) //nolint:errcheck

	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pending") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 pending file, got %d", count)
	}

	// The file should contain the second (latest) commit.
	data, _ := os.ReadFile(filepath.Join(dir, "proj.pending"))
	var task queue.Task
	json.Unmarshal(data, &task) //nolint:errcheck
	if task.After != "bbb" {
		t.Errorf("expected latest commit 'bbb', got %q", task.After)
	}
}

func TestQueueRunProcessesAndDeletes(t *testing.T) {
	dir := t.TempDir()

	processed := make(chan queue.Task, 10)
	q, err := queue.New(dir, func(task queue.Task) error {
		processed <- task
		return nil
	})
	if err != nil {
		t.Fatalf("creating queue: %v", err)
	}

	done := make(chan struct{})
	go q.Run(done)
	defer close(done)

	if err := q.Enqueue(queue.Task{Project: "test-proj", After: "deadbeef"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case task := <-processed:
		if task.Project != "test-proj" {
			t.Errorf("unexpected project: %s", task.Project)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for task to be processed")
	}

	// File should be deleted after processing.
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "test-proj.pending")); !os.IsNotExist(err) {
		t.Error("expected queue file to be deleted after processing")
	}
}
