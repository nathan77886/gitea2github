package git

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nathan77886/gitea2github/internal/config"
)

// Syncer handles cloning from Gitea and pushing to GitHub.
type Syncer struct {
	workDir string
	cfg     *config.Config
}

// New creates a Syncer that stores local repos under workDir.
func New(workDir string, cfg *config.Config) *Syncer {
	return &Syncer{workDir: workDir, cfg: cfg}
}

// Sync mirrors branches from Gitea (origin) to GitHub (github) for project p
// using a "rebase_then_force" strategy:
//
//   - Local repo is a normal (non-bare) clone created with --no-checkout.
//   - origin points at Gitea, github points at GitHub.
//   - Each run fetches both remotes with --prune.
//   - For every Gitea branch (refs/remotes/origin/*, except HEAD):
//   - if the branch does not yet exist on GitHub it is pushed as-is;
//   - otherwise the Gitea branch is checked out, rebased onto
//     refs/remotes/github/<branch> and force-pushed (with lease).
//     A rebase conflict aborts the rebase and skips that branch (logged)
//     rather than failing the entire sync.
//
// Branches that exist only on GitHub are intentionally left alone, and tags
// are not synced – this is deliberately less aggressive than push --mirror.
func (s *Syncer) Sync(p *config.Project) error {
	githubCred, err := s.cfg.FindGithubCredential(p.GithubCredential)
	if err != nil {
		return err
	}

	// New layout: non-bare clone at <workDir>/<name>. Old "<name>.git"
	// mirror directories from previous versions are no longer used; on
	// upgrade a fresh clone will be created here.
	repoDir := filepath.Join(s.workDir, p.Name)

	// Build the effective Gitea URL (embed HTTP credentials when needed).
	giteaURL, err := buildGiteaURL(p.GiteaRepo, s.cfg.Gitea)
	if err != nil {
		return fmt.Errorf("building gitea URL: %w", err)
	}

	// Gitea is HTTP/HTTPS only – no SSH env needed.
	var giteaEnv []string
	githubEnv := buildSSHEnv(githubCred.SSHKey)

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		// First time: regular clone without checking out a working tree.
		if err := s.runGit(giteaEnv, "clone", "--no-checkout", "--origin", "origin", giteaURL, repoDir); err != nil {
			return fmt.Errorf("cloning %s: %w", p.GiteaRepo, err)
		}
	}

	// Ensure both remotes exist with up-to-date URLs.
	if err := s.ensureRemote(repoDir, "origin", giteaURL, giteaEnv); err != nil {
		return fmt.Errorf("setting origin remote: %w", err)
	}
	if err := s.ensureRemote(repoDir, "github", p.GithubRepo, githubEnv); err != nil {
		return fmt.Errorf("setting github remote: %w", err)
	}

	// Fetch both sides.
	if err := s.runGitInDir(repoDir, giteaEnv, "fetch", "origin", "--prune"); err != nil {
		return fmt.Errorf("fetching origin: %w", err)
	}
	if err := s.runGitInDir(repoDir, githubEnv, "fetch", "github", "--prune"); err != nil {
		return fmt.Errorf("fetching github: %w", err)
	}

	// Enumerate Gitea branches.
	branches, err := s.listRemoteBranches(repoDir, "origin")
	if err != nil {
		return fmt.Errorf("listing origin branches: %w", err)
	}
	githubBranches, err := s.listRemoteBranches(repoDir, "github")
	if err != nil {
		return fmt.Errorf("listing github branches: %w", err)
	}
	githubHas := make(map[string]bool, len(githubBranches))
	for _, b := range githubBranches {
		githubHas[b] = true
	}

	for _, branch := range branches {
		if err := s.syncBranch(repoDir, branch, githubHas[branch], githubEnv); err != nil {
			// Per-branch errors are logged; we keep going so a single
			// problematic branch doesn't block the rest.
			log.Printf("sync project %s branch %s: %v", p.Name, branch, err)
		}
	}

	return nil
}

// syncBranch handles one branch using the rebase_then_force strategy.
func (s *Syncer) syncBranch(repoDir, branch string, existsOnGithub bool, githubEnv []string) error {
	originRef := "refs/remotes/origin/" + branch
	githubRef := "refs/remotes/github/" + branch

	if !existsOnGithub {
		// Fresh branch on GitHub – plain push, no force needed.
		return s.runGitInDir(repoDir, githubEnv, "push", "github",
			originRef+":refs/heads/"+branch)
	}

	// Branch exists on both sides: rebase Gitea tip onto GitHub tip,
	// then force-push (with lease). We need a working tree for rebase,
	// so check out a throwaway local branch at the Gitea tip.
	tmpBranch := "sync/" + branch
	if err := s.runGitInDir(repoDir, nil, "checkout", "-B", tmpBranch, originRef); err != nil {
		return fmt.Errorf("checking out %s: %w", originRef, err)
	}

	if err := s.runGitInDir(repoDir, nil, "rebase", githubRef); err != nil {
		// Abort the in-progress rebase so the repo is left clean for
		// the next branch / next sync.
		_ = s.runGitInDir(repoDir, nil, "rebase", "--abort")
		return fmt.Errorf("rebase onto %s failed, skipping: %w", githubRef, err)
	}

	// --force-with-lease guards against overwriting GitHub commits we
	// haven't seen since the last fetch.
	leaseArg := "--force-with-lease=refs/heads/" + branch + ":" + githubRef
	return s.runGitInDir(repoDir, githubEnv, "push", leaseArg, "github",
		"HEAD:refs/heads/"+branch)
}

// listRemoteBranches returns the short branch names under refs/remotes/<remote>/,
// excluding the symbolic HEAD entry.
func (s *Syncer) listRemoteBranches(repoDir, remote string) ([]string, error) {
	cmd := exec.Command("git", "for-each-ref",
		"--format=%(refname)", "refs/remotes/"+remote+"/")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git for-each-ref: %w\n%s", err, out)
	}
	prefix := "refs/remotes/" + remote + "/"
	var branches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := strings.TrimPrefix(line, prefix)
		if name == "HEAD" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// buildGiteaURL returns a URL suitable for use as a git remote.
// Gitea is only supported over HTTP/HTTPS; when a username is configured the
// credentials are embedded into the URL using net/url so that special
// characters (@, :, #, ?, …) in tokens or passwords are properly escaped.
func buildGiteaURL(rawURL string, gitea config.GiteaConfig) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing gitea_repo %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("gitea_repo %q must use http or https (Gitea SSH is not supported)", rawURL)
	}
	if gitea.Username == "" {
		return u.String(), nil
	}
	u.User = url.UserPassword(gitea.Username, gitea.Password)
	return u.String(), nil
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
