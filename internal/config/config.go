package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GiteaCredential holds authentication info for a Gitea instance.
type GiteaCredential struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // http or ssh
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSHKey   string `yaml:"ssh_key"`
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
	GiteaCredential  string `yaml:"gitea_credential"`
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
	GiteaCredentials  []GiteaCredential  `yaml:"gitea_credentials"`
	GithubCredentials []GithubCredential `yaml:"github_credentials"`
	Projects          []Project          `yaml:"projects"`
}

// Load reads and parses the YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
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

// FindGiteaCredential returns the GiteaCredential with the given name.
func (c *Config) FindGiteaCredential(name string) (*GiteaCredential, error) {
	for i := range c.GiteaCredentials {
		if c.GiteaCredentials[i].Name == name {
			return &c.GiteaCredentials[i], nil
		}
	}
	return nil, fmt.Errorf("gitea credential %q not found", name)
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
