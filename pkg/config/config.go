package config

import (
	"context"
	"strings"

	"github.com/urfave/cli/v3"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
)

// Config represents the complete speedrun configuration
type Config struct {
	GitHub  GitHubConfig
	AI      AIConfig
	Checks  ChecksConfig
	Cache   CacheConfig
	Op      OpConfig
	Backoff backoffconfig.GlobalConfig
}

// GitHubConfig holds GitHub-related configuration
type GitHubConfig struct {
	Token       string // GitHub personal access token
	SearchQuery string // GitHub search query for PRs
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
	Path       string // Cache directory path
	MaxAgeDays int    // Maximum age of cache entries in days
}

// OpConfig holds 1Password configuration
type OpConfig struct {
	Enabled bool   // Whether 1Password integration is enabled
	Account string // 1Password account
}

// LoadFromCLI loads configuration from CLI context
func LoadFromCLI(cmd *cli.Command) *Config {
	return &Config{
		GitHub: GitHubConfig{
			Token:       cmd.String("github-token"),
			SearchQuery: cmd.String("github-search-query"),
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
			Path:       cmd.String("cache-path"),
			MaxAgeDays: cmd.Int("cache-max-age-days"),
		},
		Op: OpConfig{
			Enabled: cmd.Bool("op-enable"),
			Account: cmd.String("op-account"),
		},
		Backoff: *backoffconfig.DefaultGlobalConfig(),
	}
}

// ResolveSecrets resolves any op:// references in the configuration
func (c *Config) ResolveSecrets(ctx context.Context, opClient OpClient) error {
	if !c.Op.Enabled {
		return nil
	}

	// Resolve GitHub token if it's an op:// reference
	if strings.HasPrefix(c.GitHub.Token, "op://") {
		resolved, err := opClient.Inject(ctx, c.GitHub.Token)
		if err != nil {
			return err
		}
		c.GitHub.Token = strings.TrimSpace(resolved)
	}

	// Resolve AI API key if it's an op:// reference
	if strings.HasPrefix(c.AI.APIKey, "op://") {
		resolved, err := opClient.Inject(ctx, c.AI.APIKey)
		if err != nil {
			return err
		}
		c.AI.APIKey = strings.TrimSpace(resolved)
	}

	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validation will be added as needed
	return nil
}

// OpClient interface for 1Password operations
type OpClient interface {
	Inject(ctx context.Context, template string) (string, error)
}

