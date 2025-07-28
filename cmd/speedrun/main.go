package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kennyp/speedrun/internal/ui"
	"github.com/kennyp/speedrun/pkg/agent"
	"github.com/kennyp/speedrun/pkg/cache"
	"github.com/kennyp/speedrun/pkg/config"
	"github.com/kennyp/speedrun/pkg/github"
	gap "github.com/muesli/go-app-paths"
	"github.com/urfave/cli-altsrc/v3"
	"github.com/urfave/cli/v3"
)

//go:embed example-config.toml
var defaultConfigTemplate string

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
		Name:        "speedrun",
		Usage:       "AI-powered PR review tool for on-call engineers",
		Description: "All string configuration values support 1Password references (op://vault/item/field).\n\n1Password settings are controlled via environment variables:\n  SPEEDRUN_OP_DISABLE - disable 1Password integration (any truthy value)\n  SPEEDRUN_OP_ACCOUNT or OP_ACCOUNT - specify 1Password account",
		Version:     "0.1.0",
		Authors:     []any{"Kenny Parnell <k.parnell@gmail.com>"},
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
				Usage:    "GitHub personal access token",
				Category: "GitHub",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_TOKEN"),
					config.OpTOMLValueSource("github.token", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "github-search-query",
				Usage:    "GitHub search query for PRs",
				Category: "GitHub",
				Value:    "is:open is:pr",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_SEARCH_QUERY"),
					config.OpTOMLValueSource("github.search_query", configFile),
				),
			},

			// AI settings
			&cli.BoolWithInverseFlag{
				Name:     "ai-enabled",
				Usage:    "Should AI Be Reivew RP",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_ENABLED"),
					config.OpTOMLValueSource("ai.enabled", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-base-url",
				Usage:    "AI API base URL (e.g., LLM gateway)",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BASE_URL"),
					config.OpTOMLValueSource("ai.base_url", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-api-key",
				Usage:    "AI API key",
				Category: "AI",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_API_KEY"),
					config.OpTOMLValueSource("ai.api_key", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "ai-model",
				Usage:    "AI model to use",
				Category: "AI",
				Value:    "gpt-4",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_MODEL"),
					config.OpTOMLValueSource("ai.model", configFile),
				),
			},

			// Check filtering
			&cli.StringSliceFlag{
				Name:     "checks-ignored",
				Usage:    "CI checks to ignore",
				Category: "Checks",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CHECKS_IGNORED"),
					config.OpTOMLValueSource("checks.ignored", configFile),
				),
			},
			&cli.StringSliceFlag{
				Name:     "checks-required",
				Usage:    "CI checks that must pass (if set, only these matter)",
				Category: "Checks",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CHECKS_REQUIRED"),
					config.OpTOMLValueSource("checks.required", configFile),
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
					config.OpTOMLValueSource("cache.path", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "cache-max-age",
				Usage:    "maximum age of cache entries (e.g., 7d, 24h, 168h)",
				Category: "Cache",
				Value:    7 * 24 * time.Hour, // 7 days
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CACHE_MAX_AGE"),
					config.OpTOMLValueSource("cache.max_age", configFile),
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
					config.OpTOMLValueSource("log.level", configFile),
				),
			},
			&cli.StringFlag{
				Name:     "log-path",
				Usage:    "log file path (empty for default, '-' or 'stderr' for stderr)",
				Category: "Logging",
				Value:    logPath,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_LOG_PATH"),
					config.OpTOMLValueSource("log.path", configFile),
				),
			},

			// Global backoff settings
			&cli.DurationFlag{
				Name:     "backoff-max-elapsed",
				Usage:    "Global maximum elapsed time for backoff retry (e.g., 30s, 1m)",
				Category: "Backoff",
				Value:    30 * time.Second,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_BACKOFF_MAX_ELAPSED"),
					config.OpTOMLValueSource("backoff.max_elapsed_time", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "backoff-initial-interval",
				Usage:    "Global initial backoff interval (e.g., 1s, 500ms)",
				Category: "Backoff",
				Value:    1 * time.Second,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_BACKOFF_INITIAL_INTERVAL"),
					config.OpTOMLValueSource("backoff.initial_interval", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "backoff-max-interval",
				Usage:    "Global maximum backoff interval (e.g., 10s, 30s)",
				Category: "Backoff",
				Value:    10 * time.Second,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_BACKOFF_MAX_INTERVAL"),
					config.OpTOMLValueSource("backoff.max_interval", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "backoff-multiplier",
				Usage:    "Global backoff multiplier for exponential backoff",
				Category: "Backoff",
				Value:    2.0,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_BACKOFF_MULTIPLIER"),
					config.OpTOMLValueSource("backoff.multiplier", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "backoff-randomization-factor",
				Usage:    "Global randomization factor for backoff jitter (0.0-1.0)",
				Category: "Backoff",
				Value:    0.1,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_BACKOFF_RANDOMIZATION_FACTOR"),
					config.OpTOMLValueSource("backoff.randomization_factor", configFile),
				),
			},

			// GitHub-specific backoff overrides
			&cli.DurationFlag{
				Name:     "github-backoff-max-elapsed",
				Usage:    "GitHub-specific maximum elapsed time for backoff retry (overrides global)",
				Category: "GitHub Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_BACKOFF_MAX_ELAPSED"),
					config.OpTOMLValueSource("github.backoff.max_elapsed_time", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "github-backoff-initial-interval",
				Usage:    "GitHub-specific initial backoff interval (overrides global)",
				Category: "GitHub Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_BACKOFF_INITIAL_INTERVAL"),
					config.OpTOMLValueSource("github.backoff.initial_interval", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "github-backoff-max-interval",
				Usage:    "GitHub-specific maximum backoff interval (overrides global)",
				Category: "GitHub Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_BACKOFF_MAX_INTERVAL"),
					config.OpTOMLValueSource("github.backoff.max_interval", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "github-backoff-multiplier",
				Usage:    "GitHub-specific backoff multiplier (overrides global)",
				Category: "GitHub Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_BACKOFF_MULTIPLIER"),
					config.OpTOMLValueSource("github.backoff.multiplier", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "github-backoff-randomization-factor",
				Usage:    "GitHub-specific randomization factor (overrides global)",
				Category: "GitHub Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_BACKOFF_RANDOMIZATION_FACTOR"),
					config.OpTOMLValueSource("github.backoff.randomization_factor", configFile),
				),
			},

			// AI-specific backoff overrides
			&cli.DurationFlag{
				Name:     "ai-backoff-max-elapsed",
				Usage:    "AI-specific maximum elapsed time for backoff retry (overrides global)",
				Category: "AI Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BACKOFF_MAX_ELAPSED"),
					config.OpTOMLValueSource("ai.backoff.max_elapsed_time", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "ai-backoff-initial-interval",
				Usage:    "AI-specific initial backoff interval (overrides global)",
				Category: "AI Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BACKOFF_INITIAL_INTERVAL"),
					config.OpTOMLValueSource("ai.backoff.initial_interval", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "ai-backoff-max-interval",
				Usage:    "AI-specific maximum backoff interval (overrides global)",
				Category: "AI Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BACKOFF_MAX_INTERVAL"),
					config.OpTOMLValueSource("ai.backoff.max_interval", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "ai-backoff-multiplier",
				Usage:    "AI-specific backoff multiplier (overrides global)",
				Category: "AI Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BACKOFF_MULTIPLIER"),
					config.OpTOMLValueSource("ai.backoff.multiplier", configFile),
				),
			},
			&cli.Float64Flag{
				Name:     "ai-backoff-randomization-factor",
				Usage:    "AI-specific randomization factor (overrides global)",
				Category: "AI Backoff",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_BACKOFF_RANDOMIZATION_FACTOR"),
					config.OpTOMLValueSource("ai.backoff.randomization_factor", configFile),
				),
			},

			// Client timeout settings
			&cli.DurationFlag{
				Name:     "client-timeout",
				Usage:    "Global client timeout for HTTP requests (e.g., 30s, 1m)",
				Category: "Client Timeouts",
				Value:    30 * time.Second,
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_CLIENT_TIMEOUT"),
					config.OpTOMLValueSource("client.timeout", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "github-client-timeout",
				Usage:    "GitHub-specific client timeout (overrides global)",
				Category: "Client Timeouts",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_GITHUB_CLIENT_TIMEOUT"),
					config.OpTOMLValueSource("github.client.timeout", configFile),
				),
			},
			&cli.DurationFlag{
				Name:     "ai-client-timeout",
				Usage:    "AI-specific client timeout (overrides global)",
				Category: "Client Timeouts",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AI_CLIENT_TIMEOUT"),
					config.OpTOMLValueSource("ai.client.timeout", configFile),
				),
			},

			// Auto-merge settings
			&cli.StringFlag{
				Name:     "auto-merge-on-approval",
				Usage:    "Auto-merge behavior on PR approval (true, false, ask)",
				Category: "Auto-merge",
				Value:    "ask",
				Sources: cli.NewValueSourceChain(
					cli.EnvVar("SPEEDRUN_AUTO_MERGE_ON_APPROVAL"),
					config.OpTOMLValueSource("github.auto_merge_on_approval", configFile),
				),
			},
		},
		Action: runSpeedrun,
		Commands: []*cli.Command{
			{
				Name:   "init",
				Usage:  "Initialize speedrun configuration",
				Action: initConfig,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "edit",
						Aliases: []string{"e"},
						Usage:   "open config file in $EDITOR after creation",
					},
				},
			},
		},
	}

	if err := app.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

// maskToken masks sensitive tokens for logging, showing only first 8 and last 4 characters
func maskToken(token string) string {
	if token == "" {
		return "<empty>"
	}
	if len(token) <= 12 {
		return "***"
	}
	return token[:8] + "..." + token[len(token)-4:]
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

	// Determine log output using Unix conventions
	var logWriter *os.File
	var err error

	switch cfg.Log.Path {
	case "", "default":
		// Use default log path when unset
		defaultLogPath := filepath.Join(filepath.Dir(cfg.Cache.Path), "speedrun.log")
		// Create log directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(defaultLogPath), 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}
		logWriter, err = os.OpenFile(defaultLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open default log file %s: %w", defaultLogPath, err)
		}
	case "-", "stderr":
		// Explicitly use stderr
		logWriter = os.Stderr
	default:
		// Use specified log file path
		// Create log directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(cfg.Log.Path), 0755); err != nil {
			return fmt.Errorf("failed to create log directory for %s: %w", cfg.Log.Path, err)
		}
		logWriter, err = os.OpenFile(cfg.Log.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file %s: %w", cfg.Log.Path, err)
		}
	}

	handler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Debug("Starting speedrun", "log_level", cfg.Log.Level, "log_path", cfg.Log.Path)

	// Note: 1Password references are now resolved automatically during config parsing
	// via OpTOMLValueSource, so no need for manual ResolveSecrets() call

	// Debug logging if SPEEDRUN_DEBUG is set
	if os.Getenv("SPEEDRUN_DEBUG") != "" {
		slog.Debug("Configuration after processing",
			"github.token", maskToken(cfg.GitHub.Token),
			"github.search_query", cfg.GitHub.SearchQuery,
			"ai.enabled", cfg.AI.Enabled,
			"ai.base_url", cfg.AI.BaseURL,
			"ai.api_key", maskToken(cfg.AI.APIKey),
			"ai.model", cfg.AI.Model,
			"cache.path", cfg.Cache.Path,
			"log.level", cfg.Log.Level,
			"log.path", cfg.Log.Path,
		)
	}

	// Validate configuration
	slog.Debug("Validating configuration...")
	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize cache
	slog.Debug("Initializing cache", "path", cfg.Cache.Path, "max_age", cfg.Cache.MaxAge)
	cacheInstance, err := cache.New(cfg.Cache.Path, cfg.Cache.MaxAge)
	if err != nil {
		slog.Error("Failed to initialize cache", "error", err)
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	defer func() {
		if err := cacheInstance.Close(); err != nil {
			slog.Error("Failed to close cache", slog.Any("error", err))
		}
	}()

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
	slog.Debug("GitHub checks configuration",
		slog.Any("ignored", githubChecksConfig.Ignored),
		slog.Any("required", githubChecksConfig.Required),
		slog.Int("ignored_len", len(githubChecksConfig.Ignored)),
	)
	githubClient, err := github.NewClient(ctx, cfg.GitHub.Token, cfg.GitHub.SearchQuery, cacheInstance, cfg.GitHub.Backoff, githubChecksConfig)
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
		aiAgent = agent.NewAgent(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Backoff)
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
	configExists := false
	if _, err := os.Stat(configPath); err == nil {
		configExists = true
		fmt.Printf("Config file already exists at %s\n", configPath)
	}

	// Write the embedded config template to file only if it doesn't exist
	if !configExists {
		if err := os.WriteFile(configPath, []byte(defaultConfigTemplate), 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
		fmt.Printf("Created default config at %s\n", configPath)
	}

	// Open in editor if --edit flag is provided
	if cmd.Bool("edit") {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			// Try common editors as fallbacks
			for _, fallback := range []string{"vim", "vi", "nano", "code", "subl"} {
				if _, err := exec.LookPath(fallback); err == nil {
					editor = fallback
					break
				}
			}
		}

		if editor == "" {
			fmt.Println("No suitable editor found. Please set the $EDITOR environment variable.")
			fmt.Println("Please edit the config file to add your GitHub token and AI API key.")
			return nil
		}

		fmt.Printf("Opening config in %s...\n", editor)
		cmd := exec.Command(editor, configPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Printf("Failed to open editor: %v\n", err)
			if !configExists {
				fmt.Println("Please edit the config file manually to add your GitHub token and AI API key.")
			}
		}
	} else if !configExists {
		fmt.Println("Please edit the config file to add your GitHub token and AI API key.")
	}

	return nil
}
