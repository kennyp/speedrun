package config

import (
	"time"

	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
	"github.com/urfave/cli/v3"
)

// Config represents the complete speedrun configuration
type Config struct {
	GitHub  GitHubConfig
	AI      AIConfig
	Checks  ChecksConfig
	Cache   CacheConfig
	Log     LogConfig
	Backoff backoffconfig.GlobalConfig
}

// GitHubConfig holds GitHub-related configuration
type GitHubConfig struct {
	Token               string // GitHub personal access token
	SearchQuery         string // GitHub search query for PRs
	AutoMergeOnApproval string // Auto-merge behavior on approval: "true", "false", or "ask"
}

// AIConfig holds AI/LLM configuration
type AIConfig struct {
	Enabled bool   // Should AI Reivew the PR
	BaseURL string // LLM Gateway or API base URL
	APIKey  string // API key for authentication
	Model   string // Model to use (e.g., gpt-4)
}

// ChecksConfig holds CI check filtering configuration
type ChecksConfig struct {
	Ignored  []string // Checks to ignore
	Required []string // If set, only these checks matter
}

// CacheConfig holds cache-related configuration
type CacheConfig struct {
	Path   string        // Cache directory path
	MaxAge time.Duration // Maximum age of cache entries (e.g., 7*24*time.Hour)
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level string // Log level (debug, info, warn, error)
	Path  string // Log file path (empty for stderr)
}

// LoadFromCLI loads configuration from CLI context
func LoadFromCLI(cmd *cli.Command) *Config {
	return &Config{
		GitHub: GitHubConfig{
			Token:               cmd.String("github-token"),
			SearchQuery:         cmd.String("github-search-query"),
			AutoMergeOnApproval: cmd.String("auto-merge-on-approval"),
		},
		AI: AIConfig{
			Enabled: cmd.Bool("ai-enabled"),
			BaseURL: cmd.String("ai-base-url"),
			APIKey:  cmd.String("ai-api-key"),
			Model:   cmd.String("ai-model"),
		},
		Checks: ChecksConfig{
			Ignored:  cmd.StringSlice("checks-ignored"),
			Required: cmd.StringSlice("checks-required"),
		},
		Cache: CacheConfig{
			Path:   cmd.String("cache-path"),
			MaxAge: cmd.Duration("cache-max-age"),
		},
		Log: LogConfig{
			Level: cmd.String("log-level"),
			Path:  cmd.String("log-path"),
		},
		Backoff: *backoffconfig.DefaultGlobalConfig(),
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validation will be added as needed
	return nil
}
