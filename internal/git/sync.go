package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nathan77886/gitea2github/internal/config"
)

// Syncer handles cloning from Gitea and pushing to GitHub.
type Syncer struct {
	workDir string
	cfg     *config.Config
}

// New creates a Syncer that stores local bare repos under workDir.
func New(workDir string, cfg *config.Config) *Syncer {
	return &Syncer{workDir: workDir, cfg: cfg}
}

// Sync performs a fetch from Gitea and a mirror-push to GitHub for project p.
func (s *Syncer) Sync(p *config.Project) error {
	giteaCred, err := s.cfg.FindGiteaCredential(p.GiteaCredential)
	if err != nil {
		return err
	}
	githubCred, err := s.cfg.FindGithubCredential(p.GithubCredential)
	if err != nil {
		return err
	}

	repoDir := filepath.Join(s.workDir, p.Name+".git")

	// Build the effective Gitea URL (embed HTTP credentials when needed).
	giteaURL, err := buildGiteaURL(p.GiteaRepo, giteaCred)
	if err != nil {
		return fmt.Errorf("building gitea URL: %w", err)
	}

	// SSH environment for Gitea (used when type == ssh).
	giteaEnv := buildSSHEnv(giteaCred.SSHKey)

	// SSH environment for GitHub.
	githubEnv := buildSSHEnv(githubCred.SSHKey)

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// First time: bare-clone from Gitea.
		if err := s.runGit(giteaEnv, "clone", "--mirror", giteaURL, repoDir); err != nil {
			return fmt.Errorf("cloning %s: %w", p.GiteaRepo, err)
		}
	} else {
		// Subsequent: fetch all updates from Gitea.
		if err := s.runGitInDir(repoDir, giteaEnv, "fetch", "--all", "--prune"); err != nil {
			return fmt.Errorf("fetching %s: %w", p.GiteaRepo, err)
		}
	}

	// Ensure the github remote exists.
	if err := s.ensureRemote(repoDir, "github", p.GithubRepo, githubEnv); err != nil {
		return fmt.Errorf("setting github remote: %w", err)
	}

	// Mirror-push to GitHub.
	if err := s.runGitInDir(repoDir, githubEnv, "push", "github", "--mirror"); err != nil {
		return fmt.Errorf("pushing to GitHub: %w", err)
	}

	return nil
}

// buildGiteaURL returns a URL suitable for use as a git remote.
// For HTTP credentials the username:password is embedded; for SSH the URL is
// returned unchanged (authentication is handled via GIT_SSH_COMMAND).
func buildGiteaURL(rawURL string, cred *config.GiteaCredential) (string, error) {
	if cred.Type == "ssh" {
		return rawURL, nil
	}
	// HTTP – embed credentials.
	if cred.Username == "" {
		return rawURL, nil
	}
	// Inject user:pass into the URL.
	// e.g. https://gitea.example.com/owner/repo
	//   -> https://user:pass@gitea.example.com/owner/repo
	for _, scheme := range []string{"https://", "http://"} {
		if len(rawURL) > len(scheme) && rawURL[:len(scheme)] == scheme {
			return scheme + cred.Username + ":" + cred.Password + "@" + rawURL[len(scheme):], nil
		}
	}
	return rawURL, nil
}

// buildSSHEnv returns the environment additions needed to use a specific SSH key.
// Returns nil when keyPath is empty (fall back to the default SSH agent/key).
func buildSSHEnv(keyPath string) []string {
	if keyPath == "" {
		return nil
	}
	return []string{
		"GIT_SSH_COMMAND=ssh -i " + keyPath + " -o StrictHostKeyChecking=no -o BatchMode=yes",
	}
}

// ensureRemote adds the remote if it does not exist, or updates its URL.
func (s *Syncer) ensureRemote(repoDir, name, url string, env []string) error {
	// Check whether the remote exists.
	cmd := exec.Command("git", "remote", "get-url", name)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		// Remote doesn't exist – add it.
		return s.runGitInDir(repoDir, env, "remote", "add", name, url)
	}
	// Remote exists – update URL in case it changed.
	return s.runGitInDir(repoDir, env, "remote", "set-url", name, url)
}

// runGit runs a git command without a working directory.
func (s *Syncer) runGit(extraEnv []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// runGitInDir runs a git command inside repoDir.
func (s *Syncer) runGitInDir(repoDir string, extraEnv []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}
