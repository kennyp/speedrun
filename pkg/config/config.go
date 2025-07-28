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
	Client  ClientConfig
	Backoff backoffconfig.GlobalConfig
}

// GitHubConfig holds GitHub-related configuration
type GitHubConfig struct {
	Token               string               // GitHub personal access token
	SearchQuery         string               // GitHub search query for PRs
	AutoMergeOnApproval string               // Auto-merge behavior on approval: "true", "false", or "ask"
	Backoff             backoffconfig.Config // GitHub-specific backoff overrides
	Client              ClientTimeoutConfig  // GitHub-specific client settings
}

// AIConfig holds AI/LLM configuration
type AIConfig struct {
	Enabled         bool                 // Should AI Reivew the PR
	BaseURL         string               // LLM Gateway or API base URL
	APIKey          string               // API key for authentication
	Model           string               // Model to use (e.g., gpt-4)
	AnalysisTimeout time.Duration        // Timeout for entire AI analysis conversation
	ToolTimeout     time.Duration        // Timeout for individual tool executions
	Backoff         backoffconfig.Config // AI-specific backoff overrides
	Client          ClientTimeoutConfig  // AI-specific client settings
}

// ChecksConfig holds CI check filtering configuration
type ChecksConfig struct {
	Ignored  []string // Checks to ignore
	Required []string // If set, only these checks matter
}

// CacheConfig holds cache-related configuration
type CacheConfig struct {
	Enabled bool          // Whether caching is enabled
	Path    string        // Cache directory path
	MaxAge  time.Duration // Maximum age of cache entries (e.g., 7*24*time.Hour)
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level string // Log level (debug, info, warn, error)
	Path  string // Log file path (empty for stderr)
}

// ClientConfig holds global client configuration
type ClientConfig struct {
	Timeout time.Duration // Global client timeout for HTTP requests
}

// ClientTimeoutConfig holds service-specific client timeout configuration
type ClientTimeoutConfig struct {
	Timeout time.Duration // Service-specific client timeout (overrides global)
}

// LoadFromCLI loads configuration from CLI context with hierarchical inheritance
func LoadFromCLI(cmd *cli.Command) *Config {
	// Build global backoff config from CLI flags
	globalBackoff := backoffconfig.Config{
		MaxElapsedTime:      cmd.Duration("backoff-max-elapsed"),
		InitialInterval:     cmd.Duration("backoff-initial-interval"),
		MaxInterval:         cmd.Duration("backoff-max-interval"),
		Multiplier:          cmd.Float64("backoff-multiplier"),
		RandomizationFactor: cmd.Float64("backoff-randomization-factor"),
	}

	// Build GitHub-specific backoff config with inheritance from global
	githubBackoff := backoffconfig.Config{
		MaxElapsedTime:      getDurationWithFallback(cmd, "github-backoff-max-elapsed", globalBackoff.MaxElapsedTime),
		InitialInterval:     getDurationWithFallback(cmd, "github-backoff-initial-interval", globalBackoff.InitialInterval),
		MaxInterval:         getDurationWithFallback(cmd, "github-backoff-max-interval", globalBackoff.MaxInterval),
		Multiplier:          getFloat64WithFallback(cmd, "github-backoff-multiplier", globalBackoff.Multiplier),
		RandomizationFactor: getFloat64WithFallback(cmd, "github-backoff-randomization-factor", globalBackoff.RandomizationFactor),
	}

	// Build AI-specific backoff config with inheritance from global
	aiBackoff := backoffconfig.Config{
		MaxElapsedTime:      getDurationWithFallback(cmd, "ai-backoff-max-elapsed", globalBackoff.MaxElapsedTime),
		InitialInterval:     getDurationWithFallback(cmd, "ai-backoff-initial-interval", globalBackoff.InitialInterval),
		MaxInterval:         getDurationWithFallback(cmd, "ai-backoff-max-interval", globalBackoff.MaxInterval),
		Multiplier:          getFloat64WithFallback(cmd, "ai-backoff-multiplier", globalBackoff.Multiplier),
		RandomizationFactor: getFloat64WithFallback(cmd, "ai-backoff-randomization-factor", globalBackoff.RandomizationFactor),
	}

	// Build global client config
	globalClientTimeout := cmd.Duration("client-timeout")

	// Build service-specific client configs with inheritance
	githubClientTimeout := getDurationWithFallback(cmd, "github-client-timeout", globalClientTimeout)
	aiClientTimeout := getDurationWithFallback(cmd, "ai-client-timeout", globalClientTimeout)

	checksIgnored := cmd.StringSlice("checks-ignored")
	checksRequired := cmd.StringSlice("checks-required")

	return &Config{
		GitHub: GitHubConfig{
			Token:               cmd.String("github-token"),
			SearchQuery:         cmd.String("github-search-query"),
			AutoMergeOnApproval: cmd.String("auto-merge-on-approval"),
			Backoff:             githubBackoff,
			Client:              ClientTimeoutConfig{Timeout: githubClientTimeout},
		},
		AI: AIConfig{
			Enabled:         cmd.Bool("ai-enabled"),
			BaseURL:         cmd.String("ai-base-url"),
			APIKey:          cmd.String("ai-api-key"),
			Model:           cmd.String("ai-model"),
			AnalysisTimeout: cmd.Duration("ai-analysis-timeout"),
			ToolTimeout:     cmd.Duration("ai-tool-timeout"),
			Backoff:         aiBackoff,
			Client:          ClientTimeoutConfig{Timeout: aiClientTimeout},
		},
		Checks: ChecksConfig{
			Ignored:  checksIgnored,
			Required: checksRequired,
		},
		Cache: CacheConfig{
			Enabled: cmd.Bool("cache-enabled"),
			Path:    cmd.String("cache-path"),
			MaxAge:  cmd.Duration("cache-max-age"),
		},
		Log: LogConfig{
			Level: cmd.String("log-level"),
			Path:  cmd.String("log-path"),
		},
		Client: ClientConfig{
			Timeout: globalClientTimeout,
		},
		Backoff: backoffconfig.GlobalConfig{
			Default: globalBackoff,
			GitHub:  githubBackoff,
			OpenAI:  aiBackoff,
		},
	}
}

// getDurationWithFallback returns the CLI value if set, otherwise returns the fallback
func getDurationWithFallback(cmd *cli.Command, flagName string, fallback time.Duration) time.Duration {
	if cmd.IsSet(flagName) {
		return cmd.Duration(flagName)
	}
	return fallback
}

// getFloat64WithFallback returns the CLI value if set, otherwise returns the fallback
func getFloat64WithFallback(cmd *cli.Command, flagName string, fallback float64) float64 {
	if cmd.IsSet(flagName) {
		return cmd.Float64(flagName)
	}
	return fallback
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validation will be added as needed
	return nil
}
