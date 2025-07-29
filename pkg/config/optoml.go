package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/kennyp/speedrun/pkg/op"
	altsrc "github.com/urfave/cli-altsrc/v3"
)

// 1Password processing cache to avoid repeated processing during CLI flag parsing
var (
	opProcessingCache      map[string]string // maps raw TOML content -> processed content
	opProcessingCacheMutex sync.RWMutex
)

func init() {
	opProcessingCache = make(map[string]string)
}

// OpTOMLValueSource creates a value source that processes 1Password references
// before parsing TOML and properly handles slice types for urfave/cli compatibility.
func OpTOMLValueSource(key string, source altsrc.Sourcer) *OpValueSource {
	return &OpValueSource{
		ValueSource: altsrc.NewValueSource(opTOMLUnmarshal, "op-toml", key, source),
		key:         key,
		source:      source,
	}
}

// OpValueSource wraps altsrc.ValueSource to provide proper slice handling
// and 1Password processing for TOML configuration files.
type OpValueSource struct {
	*altsrc.ValueSource
	key    string
	source altsrc.Sourcer
}

// Lookup implements the ValueSource interface with proper slice handling.
// It processes 1Password references and converts TOML slices to comma-separated
// strings that urfave/cli StringSlice flags can parse correctly.
//
// This custom implementation fixes a bug in urfave/cli-altsrc where TOML arrays
// like ["yourcompany/compliance"] get converted to the literal string "[yourcompany/compliance]"
// instead of being parsed as slice elements. See: https://github.com/urfave/cli-altsrc/issues/24
//
// Our solution uses a marshal/unmarshal technique to detect slice types and convert
// them to comma-separated strings that StringSlice flags can parse naturally.
func (ovs *OpValueSource) Lookup() (string, bool) {
	// Use our 1Password-aware TOML unmarshal function
	maafsc := altsrc.NewMapAnyAnyURISourceCache(ovs.source.SourceURI(), opTOMLUnmarshal)
	if v, ok := altsrc.NestedVal(ovs.key, maafsc.Get()); ok {
		// Try to handle as slice using marshal/unmarshal technique
		vv := struct{ Value any }{v}
		raw, err := toml.Marshal(vv)
		if err != nil {
			return fmt.Sprintf("%[1]v", v), ok
		}

		// Try to unmarshal as []string
		vvs := struct{ Value []string }{}
		if err := toml.Unmarshal(raw, &vvs); err == nil {
			return strings.Join(vvs.Value, ","), ok
		}

		// Try to unmarshal as []int
		vvi := struct{ Value []int }{}
		if err := toml.Unmarshal(raw, &vvi); err == nil {
			ss := make([]string, len(vvi.Value))
			for i := range vvi.Value {
				ss[i] = strconv.Itoa(vvi.Value[i])
			}
			return strings.Join(ss, ","), ok
		}

		// Fall back to standard string representation for non-slice types
		return fmt.Sprintf("%[1]v", v), ok
	}

	return "", false
}

// opTOMLUnmarshal processes 1Password references in TOML data and then uses
// the official toml.Unmarshal for proper type handling including slices.
func opTOMLUnmarshal(data []byte, v any) error {

	// Check if 1Password is enabled via environment variable
	opEnabled := getOpEnabledFromEnv()
	opAccount := getOpAccountFromEnv()

	if !opEnabled {
		slog.Debug("1Password integration disabled, using raw TOML")
		return toml.Unmarshal(data, v)
	}

	rawContent := string(data)

	// Check cache first to avoid repeated 1Password processing
	opProcessingCacheMutex.RLock()
	if cachedContent, exists := opProcessingCache[rawContent]; exists {
		opProcessingCacheMutex.RUnlock()
		slog.Debug("Using cached 1Password-processed TOML content")
		return toml.Unmarshal([]byte(cachedContent), v)
	}
	opProcessingCacheMutex.RUnlock()

	slog.Debug("1Password integration enabled, processing TOML file", "account", opAccount)

	// Process 1Password references in the raw TOML data
	processedData, err := processOpReferences(rawContent, opAccount)
	if err != nil {
		slog.Error("Failed to process 1Password references", "error", err)
		// Fall back to original data if op processing fails
		return toml.Unmarshal(data, v)
	}

	// Cache the processed result
	opProcessingCacheMutex.Lock()
	opProcessingCache[rawContent] = processedData
	opProcessingCacheMutex.Unlock()

	slog.Debug("Successfully processed 1Password references in TOML")
	return toml.Unmarshal([]byte(processedData), v)
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
