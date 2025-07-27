package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	gap "github.com/muesli/go-app-paths"
	"github.com/kennyp/speedrun/internal/ui"
	"github.com/kennyp/speedrun/pkg/agent"
	"github.com/kennyp/speedrun/pkg/cache"
	"github.com/kennyp/speedrun/pkg/config"
	"github.com/kennyp/speedrun/pkg/github"
	"github.com/kennyp/speedrun/pkg/op"
	"github.com/urfave/cli-altsrc/v3"
	"github.com/urfave/cli-altsrc/v3/toml"
	"github.com/urfave/cli/v3"
)

func main() {
	ctx := context.Background()

	// Create vendor-scoped paths using go-app-paths
	scope := gap.NewVendorScope(gap.User, "kennyp", "speedrun")
	
	configPath, err := scope.ConfigPath("config.toml")
	if err != nil {
		log.Fatalf("cannot get config path: %v", err)
	}
	
	cachePath, err := scope.DataPath("cache.db")
	if err != nil {
		log.Fatalf("cannot get cache path: %v", err)
	}
	
	logPath, err := scope.LogPath("speedrun.log")
	if err != nil {
		log.Fatalf("cannot get log path: %v", err)
	}

	configFile := altsrc.StringSourcer(configPath)

	app := cli.Command{
		Name:    "speedrun",
		Usage:   "AI-powered PR review tool for on-call engineers",
		Version: "0.1.0",
		Authors: []any{"Kenny Parnell <k.parnell@gmail.com>"},
		Flags: []cli.Flag{
			// Configuration
			&cli.StringFlag{
				Name:  "config",
				Usage: "config file path",
				Value: configPath,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CONFIG"),
				),
			},

			// GitHub settings
			&cli.StringFlag{
				Name:     "github-token",
				Usage:    "GitHub personal access token (or op:// reference)",
				Category: "GitHub",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_TOKEN"),
					toml.TOML("github.token", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "github-search-query",
				Usage:    "GitHub search query for PRs",
				Category: "GitHub",
				Value:    "is:open is:pr",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_SEARCH_QUERY"),
					toml.TOML("github.search_query", configFile),
				),
			},

			// AI settings
			&cli.BoolWithInverseFlag{
				Name:     "ai-enabled",
				Usage:    "Should AI Be Reivew RP",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_ENABLED"),
					toml.TOML("ai.enabled", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-base-url",
				Usage:    "AI API base URL (e.g., LLM gateway)",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BASE_URL"),
					toml.TOML("ai.base_url", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-api-key",
				Usage:    "AI API key (or op:// reference)",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_API_KEY"),
					toml.TOML("ai.api_key", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-model",
				Usage:    "AI model to use",
				Category: "AI",
				Value:    "gpt-4",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_MODEL"),
					toml.TOML("ai.model", configFile),
				),
			},

			// Check filtering
			&cli.StringSliceFlag{
				Name:     "checks-ignored",
				Usage:    "CI checks to ignore",
				Category: "Checks",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CHECKS_IGNORED"),
					toml.TOML("checks.ignored", configFile),
				),
			},
			&cli.StringSliceFlag{
				Name:     "checks-required",
				Usage:    "CI checks that must pass (if set, only these matter)",
				Category: "Checks",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CHECKS_REQUIRED"),
					toml.TOML("checks.required", configFile),
				),
			},

			// 1Password settings
			&cli.BoolWithInverseFlag{
				Name:     "op-enable",
				Usage:    "enable 1Password integration for secrets",
				Category: "1Password",
				Value:    true,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_OP_ENABLE"),
					toml.TOML("op.enabled", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "op-account",
				Usage:    "1Password account",
				Category: "1Password",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_OP_ACCOUNT"),
					cli.EnvVar("OP_ACCOUNT"),
					toml.TOML("op.account", configFile),
				),
			},

			// Cache settings
			&cli.StringFlag{
				Name:     "cache-path",
				Usage:    "cache database file path",
				Category: "Cache",
				Value:    cachePath,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CACHE_PATH"),
					toml.TOML("cache.path", configFile),
				),
			},
			&cli.IntFlag{
				Name:     "cache-max-age-days",
				Usage:    "maximum age of cache entries in days",
				Category: "Cache",
				Value:    7,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CACHE_MAX_AGE_DAYS"),
					toml.TOML("cache.max_age_days", configFile),
				),
			},

			// Logging settings
			&cli.StringFlag{
				Name:     "log-level",
				Usage:    "log level (debug, info, warn, error)",
				Category: "Logging",
				Value:    "info",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_LOG_LEVEL"),
					toml.TOML("log.level", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "log-path",
				Usage:    "log file path (empty for stderr)",
				Category: "Logging",
				Value:    logPath,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_LOG_PATH"),
					toml.TOML("log.path", configFile),
				),
			},
		},
		Action: runSpeedrun,
		Commands: []*cli.Command{
			{
				Name:   "init",
				Usage:  "Initialize speedrun configuration",
				Action: initConfig,
			},
		},
	}

	if err := app.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func runSpeedrun(ctx context.Context, cmd *cli.Command) error {
	// Load configuration from CLI first to get cache path for default log path
	cfg := config.LoadFromCLI(cmd)
	
	// Set up logging
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	
	// Determine log output
	var logWriter *os.File
	if cfg.Log.Path == "" {
		// Default to speedrun.log in cache directory
		logPath := filepath.Join(cfg.Cache.Path, "speedrun.log")
		// Create cache directory if it doesn't exist
		if err := os.MkdirAll(cfg.Cache.Path, 0755); err != nil {
			// Fall back to stderr if we can't create cache dir
			logWriter = os.Stderr
		} else {
			var err error
			logWriter, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				// Fall back to stderr if we can't open log file
				logWriter = os.Stderr
			}
		}
	} else {
		var err error
		logWriter, err = os.OpenFile(cfg.Log.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Fall back to stderr if we can't open specified log file
			logWriter = os.Stderr
		}
	}
	
	handler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	
	slog.Debug("Starting speedrun", "log_level", cfg.Log.Level, "log_path", cfg.Log.Path)

	// Resolve 1Password references if enabled
	if cfg.Op.Enabled {
		slog.Debug("1Password integration enabled")
		opClient := op.New(cfg.Op.Account)

		// Check if op CLI is available
		if !opClient.Available() {
			slog.Error("1Password CLI (op) is not installed or not in PATH")
			return fmt.Errorf("1Password CLI (op) is not installed or not in PATH")
		}

		// Sign in to 1Password
		slog.Debug("Signing in to 1Password...")
		if err := opClient.SignIn(ctx); err != nil {
			slog.Error("Failed to sign in to 1Password", "error", err)
			return fmt.Errorf("failed to sign in to 1Password: %w", err)
		}

		// Resolve secrets
		slog.Debug("Resolving secrets from 1Password...")
		if err := cfg.ResolveSecrets(ctx, opClient); err != nil {
			slog.Error("Failed to resolve secrets", "error", err)
			return fmt.Errorf("failed to resolve secrets: %w", err)
		}
		slog.Info("Successfully resolved secrets from 1Password")
	}

	// Validate configuration
	slog.Debug("Validating configuration...")
	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize cache
	slog.Debug("Initializing cache", "path", cfg.Cache.Path, "max_age_days", cfg.Cache.MaxAgeDays)
	cacheInstance, err := cache.New(cfg.Cache.Path, cfg.Cache.MaxAgeDays)
	if err != nil {
		slog.Error("Failed to initialize cache", "error", err)
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	defer cacheInstance.Close()

	// Cleanup expired cache entries on startup
	slog.Debug("Cleaning up expired cache entries...")
	if err := cacheInstance.Cleanup(); err != nil {
		slog.Warn("Failed to cleanup cache", "error", err)
		fmt.Printf("Warning: failed to cleanup cache: %v\n", err)
	}

	// Create GitHub client
	slog.Debug("Creating GitHub client", "search_query", cfg.GitHub.SearchQuery)
	githubChecksConfig := github.ChecksConfig{
		Ignored:  cfg.Checks.Ignored,
		Required: cfg.Checks.Required,
	}
	githubClient, err := github.NewClient(ctx, cfg.GitHub.Token, cfg.GitHub.SearchQuery, cacheInstance, cfg.Backoff.GitHub, githubChecksConfig)
	if err != nil {
		slog.Error("Failed to create GitHub client", "error", err)
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get authenticated user
	slog.Debug("Getting authenticated user...")
	username, err := githubClient.AuthenticatedUser(ctx)
	if err != nil {
		slog.Error("Failed to get authenticated user", "error", err)
		return fmt.Errorf("failed to get authenticated user: %w", err)
	}
	slog.Info("Successfully authenticated with GitHub", "username", username)

	fmt.Printf("🚀 Starting speedrun for %s...\n", username)
	fmt.Printf("📍 Search query: %s\n", cfg.GitHub.SearchQuery)

	// Create AI agent if configured
	var aiAgent *agent.Agent
	if cfg.AI.Enabled {
		slog.Debug("Creating AI agent", "model", cfg.AI.Model, "base_url", cfg.AI.BaseURL)
		aiAgent = agent.NewAgent(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.Model, cfg.Backoff.OpenAI)
		fmt.Printf("🤖 AI analysis enabled with model: %s\n", cfg.AI.Model)
		slog.Info("AI agent initialized", "model", cfg.AI.Model)
	} else {
		fmt.Printf("🤖 AI analysis disabled\n")
		slog.Debug("AI analysis disabled")
	}

	// Create and run the TUI
	model := ui.NewModel(ctx, cfg, githubClient, aiAgent, username)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running program: %w", err)
	}

	return nil
}

func initConfig(ctx context.Context, cmd *cli.Command) error {
	configPath := cmd.String("config")
	configDir := filepath.Dir(configPath)

	// Create config directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config file already exists at %s\n", configPath)
		return nil
	}

	// Create default config
	defaultConfig := `# Speedrun Configuration

[github]
# GitHub personal access token (or op:// reference)
# token = "ghp_..." or "op://vault/GitHub/token"
# Search query for finding PRs
search_query = "is:open is:pr org:heroku label:on-call"

[ai]
# LLM Gateway or API base URL
# base_url = "https://api.openai.com/v1"
# API key (or op:// reference)
# api_key = "sk-..." or "op://vault/OpenAI/api-key"
model = "gpt-4"

[checks]
# CI checks to ignore when determining status
ignored = ["heroku/compliance"]
# If specified, only these checks matter
# required = []

[op]
# Enable 1Password integration
enabled = true
# 1Password account (if different from default)
# account = "company.1password.com"

[cache]
# Maximum age of cache entries in days
max_age_days = 7
# Custom cache database file path (defaults to system cache dir/speedrun.db)
# path = "/custom/cache/speedrun.db"

[log]
# Log level: debug, info, warn, error
level = "info"
# Log file path (defaults to cache_dir/speedrun.log, empty for stderr)
# path = "/custom/log/path/speedrun.log"
`

	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Created default config at %s\n", configPath)
	fmt.Println("Please edit the config file to add your GitHub token and AI API key.")
	return nil
}
