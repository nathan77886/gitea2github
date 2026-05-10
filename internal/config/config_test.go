package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathan77886/gitea2github/internal/config"
)

const testYAML = `
server:
  port: 9090
  secret: global-secret

work_dir: /tmp/work
queue_dir: /tmp/queue
log_file: /tmp/sync.log

gitea:
  username: alice
  password: secret123

github_credentials:
  - name: gh-main
    ssh_key: /home/alice/.ssh/github_key

projects:
  - name: project-a
    gitea_repo: https://gitea.example.com/alice/project-a.git
    github_repo: git@github.com:alice/project-a.git
    github_credential: gh-main
    secret: proj-secret
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeConfig(t, testYAML)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("port: got %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.Secret != "global-secret" {
		t.Errorf("secret: got %q", cfg.Server.Secret)
	}
	if cfg.Gitea.Username != "alice" {
		t.Errorf("gitea username: got %q, want 'alice'", cfg.Gitea.Username)
	}
	if cfg.Gitea.Password != "secret123" {
		t.Errorf("gitea password: got %q, want 'secret123'", cfg.Gitea.Password)
	}
	if len(cfg.GithubCredentials) != 1 {
		t.Errorf("github creds: got %d, want 1", len(cfg.GithubCredentials))
	}
	if len(cfg.Projects) != 1 {
		t.Errorf("projects: got %d, want 1", len(cfg.Projects))
	}
}

func TestLoad_Defaults(t *testing.T) {
	path := writeConfig(t, "server:\n  port: 0\n")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port: got %d, want 8080", cfg.Server.Port)
	}
	if cfg.WorkDir != "work" {
		t.Errorf("default work_dir: got %q", cfg.WorkDir)
	}
	if cfg.QueueDir != "queue" {
		t.Errorf("default queue_dir: got %q", cfg.QueueDir)
	}
	if cfg.LogFile != "sync.log" {
		t.Errorf("default log_file: got %q", cfg.LogFile)
	}
}

func TestLoad_RejectsLegacyGiteaCredentials(t *testing.T) {
	legacy := `
gitea_credentials:
  - name: gitea-http
    type: http
    username: alice
    password: secret
`
	path := writeConfig(t, legacy)
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error for legacy gitea_credentials, got nil")
	}
}

func TestLoad_RejectsLegacyProjectGiteaCredential(t *testing.T) {
	legacy := `
gitea:
  username: alice
  password: secret
projects:
  - name: project-a
    gitea_repo: https://gitea.example.com/alice/project-a.git
    github_repo: git@github.com:alice/project-a.git
    gitea_credential: gitea-http
    github_credential: gh-main
`
	path := writeConfig(t, legacy)
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error for legacy project.gitea_credential, got nil")
	}
}

func TestFindGithubCredential(t *testing.T) {
	path := writeConfig(t, testYAML)
	cfg, _ := config.Load(path)

	cred, err := cfg.FindGithubCredential("gh-main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred.SSHKey != "/home/alice/.ssh/github_key" {
		t.Errorf("ssh_key: got %q", cred.SSHKey)
	}

	_, err = cfg.FindGithubCredential("missing")
	if err == nil {
		t.Error("expected error for missing github credential")
	}
}

func TestFindProjectByRepo(t *testing.T) {
	path := writeConfig(t, testYAML)
	cfg, _ := config.Load(path)

	proj := cfg.FindProjectByRepo("https://gitea.example.com/alice/project-a.git")
	if proj == nil {
		t.Fatal("expected to find project-a")
	}
	if proj.Name != "project-a" {
		t.Errorf("name: got %q, want 'project-a'", proj.Name)
	}

	if cfg.FindProjectByRepo("https://gitea.example.com/nobody/nope.git") != nil {
		t.Error("expected nil for unknown repo")
	}
}
