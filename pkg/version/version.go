package version

import (
	"os/exec"
	"runtime/debug"
	"strings"
)

// Version will be set at build time via ldflags
var Version = "dev"

// Get returns the version string, either set at build time or dynamically detected from git
func Get() string {
	if Version != "dev" {
		return formatVersion(Version)
	}

	// Try to get version info from build info (works with go install)
	if info, ok := debug.ReadBuildInfo(); ok {
		// Check if we have version info from go install
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}

		// Look for vcs.revision in build settings
		var revision string
		var modified bool
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}

		if revision != "" {
			// Try to get the tag info if available
			if tag := getLatestTag(); tag != "" {
				result := tag + "-" + revision[:7] // Use short SHA
				if modified {
					result += "(dirty)"
				}
				return result
			}
			// Fallback to just the revision
			result := revision[:7]
			if modified {
				result += "(dirty)"
			}
			return result
		}
	}

	// Final fallback: try to detect version from git at runtime
	return detectGitVersion()
}

// getLatestTag attempts to get the latest git tag
func getLatestTag() string {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// detectGitVersion attempts to get version from git describe
func detectGitVersion() string {
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty")
	output, err := cmd.Output()
	if err != nil {
		return "dev"
	}

	return formatVersion(strings.TrimSpace(string(output)))
}

// formatVersion converts git describe output to our desired format
func formatVersion(gitDescribe string) string {
	// Handle dirty working tree
	dirty := strings.HasSuffix(gitDescribe, "-dirty")
	if dirty {
		gitDescribe = strings.TrimSuffix(gitDescribe, "-dirty")
	}

	// Split on hyphens to parse git describe output
	parts := strings.Split(gitDescribe, "-")

	switch len(parts) {
	case 1:
		// Just a tag (e.g., "v0.1.0") or just a commit hash
		result := parts[0]
		if dirty {
			result += "(dirty)"
		}
		return result

	case 3:
		// Full git describe format: tag-count-ghash (e.g., "v0.1.0-3-gafaf234")
		tag := parts[0]
		commitHash := parts[2]
		// Remove the 'g' prefix from commit hash
		commitHash = strings.TrimPrefix(commitHash, "g")
		result := tag + "-" + commitHash
		if dirty {
			result += "(dirty)"
		}
		return result

	default:
		// Fallback: return as-is with dirty suffix if applicable
		result := gitDescribe
		if dirty {
			result += "(dirty)"
		}
		return result
	}
}
