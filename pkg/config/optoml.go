package config

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	altsrc "github.com/urfave/cli-altsrc/v3"
	"github.com/kennyp/speedrun/pkg/op"
)

// OpTOMLValueSource creates a value source that processes 1Password references
// before parsing TOML. It checks environment variables for 1Password settings
// to avoid circular dependencies with CLI flags.
func OpTOMLValueSource(key string, source altsrc.Sourcer) *altsrc.ValueSource {
	return altsrc.NewValueSource(opTOMLUnmarshal, "op-toml", key, source)
}

// opTOMLUnmarshal unmarshals TOML data, processing 1Password references first if enabled
func opTOMLUnmarshal(data []byte, v any) error {
	// Check if 1Password is enabled via environment variable
	// This avoids circular dependency with CLI flags
	opEnabled := getOpEnabledFromEnv()
	opAccount := getOpAccountFromEnv()
	
	if opEnabled {
		slog.Debug("1Password integration enabled, processing TOML file", "account", opAccount)
		
		// Process 1Password references in the raw TOML data
		processedData, err := processOpReferences(string(data), opAccount)
		if err != nil {
			slog.Error("Failed to process 1Password references", "error", err)
			// Fall back to original data if op processing fails
			return toml.Unmarshal(data, v)
		}
		data = []byte(processedData)
		slog.Debug("Successfully processed 1Password references in TOML")
	}
	
	return toml.Unmarshal(data, v)
}

// getOpEnabledFromEnv checks if 1Password is enabled via environment variables
// Uses SPEEDRUN_OP_DISABLE - if set to any truthy value, 1Password is disabled
func getOpEnabledFromEnv() bool {
	// Check if 1Password is explicitly disabled
	if val := os.Getenv("SPEEDRUN_OP_DISABLE"); val != "" {
		disabled, err := strconv.ParseBool(val)
		if err != nil {
			slog.Debug("Invalid SPEEDRUN_OP_DISABLE value, treating as false", "value", val, "error", err)
			return true // If we can't parse, default to enabled
		}
		return !disabled // If disabled=true, return false (not enabled)
	}
	
	// Default to enabled if SPEEDRUN_OP_DISABLE is not set
	return true
}

// getOpAccountFromEnv gets the 1Password account from environment variables
func getOpAccountFromEnv() string {
	// Check speedrun-specific env var first
	if account := os.Getenv("SPEEDRUN_OP_ACCOUNT"); account != "" {
		return account
	}
	
	// Fall back to standard 1Password env var
	return os.Getenv("OP_ACCOUNT")
}

// processOpReferences processes all op:// references in the TOML content
func processOpReferences(tomlContent, account string) (string, error) {
	// Only process if there are op:// references in the content
	if !strings.Contains(tomlContent, "op://") {
		return tomlContent, nil
	}
	
	ctx := context.Background()
	opClient := op.New(account)
	
	// Check if op CLI is available
	if !opClient.Available() {
		slog.Warn("1Password CLI (op) is not available, skipping op:// processing")
		return tomlContent, nil
	}
	
	// Sign in to 1Password
	if err := opClient.SignIn(ctx); err != nil {
		slog.Warn("Failed to sign in to 1Password, skipping op:// processing", "error", err)
		return tomlContent, nil
	}
	
	// Inject all op:// references at once
	resolved, err := opClient.Inject(ctx, tomlContent)
	if err != nil {
		return "", err
	}
	
	return resolved, nil
}