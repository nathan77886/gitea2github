package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GiteaConfig holds the global HTTP credentials used for every Gitea repo.
// Gitea is only supported over HTTP/HTTPS; the username/password (or token)
// are embedded into the clone URL at sync time.
type GiteaConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// GithubCredential holds authentication info for a GitHub account.
type GithubCredential struct {
	Name   string `yaml:"name"`
	SSHKey string `yaml:"ssh_key"`
}

// Project defines one Gitea→GitHub mirror mapping.
type Project struct {
	Name             string `yaml:"name"`
	GiteaRepo        string `yaml:"gitea_repo"`
	GithubRepo       string `yaml:"github_repo"`
	GithubCredential string `yaml:"github_credential"`
	Secret           string `yaml:"secret"` // per-project webhook secret (optional)
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port   int    `yaml:"port"`
	Secret string `yaml:"secret"` // global webhook secret (optional)
}

// Config is the top-level configuration.
type Config struct {
	Server            ServerConfig       `yaml:"server"`
	WorkDir           string             `yaml:"work_dir"`
	QueueDir          string             `yaml:"queue_dir"`
	LogFile           string             `yaml:"log_file"`
	Gitea             GiteaConfig        `yaml:"gitea"`
	GithubCredentials []GithubCredential `yaml:"github_credentials"`
	Projects          []Project          `yaml:"projects"`
}

// legacyConfig is used to detect old configuration files that still contain
// the removed gitea_credentials / project.gitea_credential fields, so we can
// fail fast with a clear migration message instead of silently ignoring them.
type legacyConfig struct {
	GiteaCredentials []map[string]any `yaml:"gitea_credentials"`
	Projects         []struct {
		Name            string `yaml:"name"`
		GiteaCredential string `yaml:"gitea_credential"`
	} `yaml:"projects"`
}

// Load reads and parses the YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var legacy legacyConfig
	if err := yaml.Unmarshal(data, &legacy); err == nil {
		if len(legacy.GiteaCredentials) > 0 {
			return nil, fmt.Errorf("config: 'gitea_credentials' is no longer supported; " +
				"replace it with a single global 'gitea: {username, password}' block")
		}
		for _, p := range legacy.Projects {
			if p.GiteaCredential != "" {
				return nil, fmt.Errorf("config: project %q uses removed field 'gitea_credential'; "+
					"remove it and configure the global 'gitea' block instead", p.Name)
			}
		}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "work"
	}
	if cfg.QueueDir == "" {
		cfg.QueueDir = "queue"
	}
	if cfg.LogFile == "" {
		cfg.LogFile = "sync.log"
	}
	return &cfg, nil
}

// FindGithubCredential returns the GithubCredential with the given name.
func (c *Config) FindGithubCredential(name string) (*GithubCredential, error) {
	for i := range c.GithubCredentials {
		if c.GithubCredentials[i].Name == name {
			return &c.GithubCredentials[i], nil
		}
	}
	return nil, fmt.Errorf("github credential %q not found", name)
}

// FindProjectByRepo returns the first project whose Gitea repo URL matches.
func (c *Config) FindProjectByRepo(repoURL string) *Project {
	for i := range c.Projects {
		if c.Projects[i].GiteaRepo == repoURL {
			return &c.Projects[i]
		}
	}
	return nil
}
