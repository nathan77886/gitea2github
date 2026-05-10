package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/nathan77886/gitea2github/internal/config"
	"github.com/nathan77886/gitea2github/internal/queue"
)

// giteaPayload is a minimal representation of a Gitea push webhook body.
type giteaPayload struct {
	Ref  string `json:"ref"`
	After string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
		SSHURL   string `json:"ssh_url"`
	} `json:"repository"`
}

// Webhook handles incoming Gitea push webhook requests.
type Webhook struct {
	cfg *config.Config
	q   *queue.Queue
}

// New creates a Webhook handler.
func New(cfg *config.Config, q *queue.Queue) *Webhook {
	return &Webhook{cfg: cfg, q: q}
}

// ServeHTTP implements http.Handler.
func (wh *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	var payload giteaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Determine the Gitea repository clone URL that was configured.
	// We try the HTTP clone URL first; if no project matches, try the SSH URL.
	project := wh.cfg.FindProjectByRepo(payload.Repository.CloneURL)
	if project == nil {
		project = wh.cfg.FindProjectByRepo(payload.Repository.SSHURL)
	}
	if project == nil {
		// Not a tracked repository – ignore.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Validate HMAC-SHA256 signature when a secret is configured.
	secret := project.Secret
	if secret == "" {
		secret = wh.cfg.Server.Secret
	}
	if secret != "" {
		sig := r.Header.Get("X-Gitea-Signature")
		if !validSignature(secret, body, sig) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	// Enqueue the sync task.
	task := queue.Task{
		Project:   project.Name,
		GiteaRepo: project.GiteaRepo,
		Ref:       payload.Ref,
		After:     payload.After,
	}
	if err := wh.q.Enqueue(task); err != nil {
		fmt.Printf("webhook: enqueue error: %v\n", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// validSignature verifies that sig equals HMAC-SHA256(secret, body).
func validSignature(secret string, body []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
