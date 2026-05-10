package handler_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nathan77886/gitea2github/internal/config"
	"github.com/nathan77886/gitea2github/internal/handler"
	"github.com/nathan77886/gitea2github/internal/queue"
)

func buildConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Projects: []config.Project{
			{
				Name:             "my-project",
				GiteaRepo:        "https://gitea.example.com/owner/my-project.git",
				GithubRepo:       "git@github.com:owner/my-project.git",
				GiteaCredential:  "gitea-http",
				GithubCredential: "github-ssh",
				Secret:           "test-secret",
			},
		},
	}
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func makePayload(cloneURL string) []byte {
	p := map[string]interface{}{
		"ref":   "refs/heads/main",
		"after": "abc1234",
		"repository": map[string]string{
			"full_name": "owner/my-project",
			"clone_url": cloneURL,
			"ssh_url":   "git@gitea.example.com:owner/my-project.git",
		},
	}
	data, _ := json.Marshal(p)
	return data
}

func newWebhook(t *testing.T, cfg *config.Config) (*handler.Webhook, *queue.Queue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := queue.New(dir, func(queue.Task) error { return nil })
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return handler.New(cfg, q), q, dir
}

func TestWebhook_AcceptsValidRequest(t *testing.T) {
	cfg := buildConfig()
	wh, _, _ := newWebhook(t, cfg)

	body := makePayload("https://gitea.example.com/owner/my-project.git")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitea-Signature", sign("test-secret", body))

	rr := httptest.NewRecorder()
	wh.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWebhook_RejectsInvalidSignature(t *testing.T) {
	cfg := buildConfig()
	wh, _, _ := newWebhook(t, cfg)

	body := makePayload("https://gitea.example.com/owner/my-project.git")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitea-Signature", "badsignature")

	rr := httptest.NewRecorder()
	wh.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestWebhook_IgnoresUnknownRepo(t *testing.T) {
	cfg := buildConfig()
	wh, _, _ := newWebhook(t, cfg)

	body := makePayload("https://gitea.example.com/owner/unknown-repo.git")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	wh.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
}

func TestWebhook_RejectsNonPost(t *testing.T) {
	cfg := buildConfig()
	wh, _, _ := newWebhook(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rr := httptest.NewRecorder()
	wh.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestWebhook_EnqueuesTask(t *testing.T) {
	cfg := buildConfig()
	dir := t.TempDir()

	received := make(chan queue.Task, 1)
	q, err := queue.New(dir, func(task queue.Task) error {
		received <- task
		return nil
	})
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	wh := handler.New(cfg, q)

	body := makePayload("https://gitea.example.com/owner/my-project.git")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Gitea-Signature", sign("test-secret", body))

	rr := httptest.NewRecorder()
	wh.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	done := make(chan struct{})
	go q.Run(done)
	defer close(done)

	select {
	case task := <-received:
		if task.Project != "my-project" {
			t.Errorf("expected project 'my-project', got %q", task.Project)
		}
		if task.After != "abc1234" {
			t.Errorf("expected after 'abc1234', got %q", task.After)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout: task was not processed")
	}
}
