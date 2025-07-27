package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kennyp/speedrun/internal/ui"
	"github.com/kennyp/speedrun/pkg/config"
	"github.com/kennyp/speedrun/pkg/github"
	"github.com/kennyp/speedrun/pkg/op"
	"github.com/urfave/cli-altsrc/v3"
	"github.com/urfave/cli-altsrc/v3/toml"
	"github.com/urfave/cli/v3"
)

func main() {
	ctx := context.Background()

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("cannot find config dir: %v", err)
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Fatalf("cannot find cache dir: %v", err)
	}

	configFile := altsrc.StringSourcer(filepath.Join(configDir, "speedrun", "config.toml"))

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
				Value: filepath.Join(configDir, "speedrun", "config.toml"),
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
				Usage:    "cache directory path",
				Category: "Cache",
				Value:    filepath.Join(cacheDir, "speedrun"),
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
	// Load configuration from CLI
	cfg := config.LoadFromCLI(cmd)

	// Resolve 1Password references if enabled
	if cfg.Op.Enabled {
		opClient := op.New(cfg.Op.Account)
		
		// Check if op CLI is available
		if !opClient.Available() {
			return fmt.Errorf("1Password CLI (op) is not installed or not in PATH")
		}

		// Sign in to 1Password
		if err := opClient.SignIn(ctx); err != nil {
			return fmt.Errorf("failed to sign in to 1Password: %w", err)
		}

		// Resolve secrets
		if err := cfg.ResolveSecrets(ctx, opClient); err != nil {
			return fmt.Errorf("failed to resolve secrets: %w", err)
		}
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create GitHub client
	githubClient, err := github.NewClient(ctx, cfg.GitHub.Token, cfg.GitHub.SearchQuery)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get authenticated user
	username, err := githubClient.AuthenticatedUser(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authenticated user: %w", err)
	}

	fmt.Printf("🚀 Starting speedrun for %s...\n", username)
	fmt.Printf("📍 Search query: %s\n", cfg.GitHub.SearchQuery)
	
	// Create and run the TUI
	model := ui.NewModel(ctx, cfg, githubClient, username)
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
# Custom cache path (defaults to system cache dir)
# path = "/custom/cache/path"
`

	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Created default config at %s\n", configPath)
	fmt.Println("Please edit the config file to add your GitHub token and AI API key.")
	return nil
}
