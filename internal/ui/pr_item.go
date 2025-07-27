package ui

import (
	"fmt"

	"github.com/kennyp/speedrun/pkg/github"
)

// PRItem represents a PR in the list
type PRItem struct {
	PR          *github.PullRequest
	DiffStats   *github.DiffStats
	CheckStatus *github.CheckStatus
	Reviews     []*github.Review
	
	// Loading states
	LoadingDiff   bool
	LoadingChecks bool
	LoadingReviews bool
	
	// Completion states
	Approved bool
	Reviewed bool // Has the current user reviewed this PR?
	
	// Errors
	DiffError    error
	CheckError   error
	ReviewError  error
}

// Title implements list.Item
func (i PRItem) Title() string {
	status := "📊"
	if i.Approved {
		status = "✅"
	} else if i.Reviewed {
		status = "👀"
	}
	return fmt.Sprintf("%s PR #%d: %s", status, i.PR.Number, i.PR.Title)
}

// Description implements list.Item
func (i PRItem) Description() string {
	// Show loading state
	if i.LoadingDiff || i.LoadingChecks || i.LoadingReviews {
		loadingText := "🔄 Loading"
		parts := []string{}
		if i.LoadingDiff {
			parts = append(parts, "diff")
		}
		if i.LoadingChecks {
			parts = append(parts, "checks")
		}
		if i.LoadingReviews {
			parts = append(parts, "reviews")
		}
		if len(parts) > 0 {
			loadingText += " " + joinWithCommas(parts) + "..."
		}
		return loadingText + " (navigation available)"
	}

	// Show errors if any
	if i.DiffError != nil || i.CheckError != nil || i.ReviewError != nil {
		return "⚠️ Error loading PR data"
	}

	// Build description from available data
	desc := ""
	
	// Diff stats
	if i.DiffStats != nil {
		desc += fmt.Sprintf("📊 +%d/-%d lines, %d files",
			i.DiffStats.Additions, i.DiffStats.Deletions, i.DiffStats.Files)
	}
	
	// Check status
	if i.CheckStatus != nil {
		if desc != "" {
			desc += " | "
		}
		emoji := getStatusEmoji(i.CheckStatus.State)
		desc += fmt.Sprintf("🔧 %s%s", emoji, i.CheckStatus.Description)
	}
	
	// Reviews
	if len(i.Reviews) > 0 {
		if desc != "" {
			desc += " | "
		}
		desc += fmt.Sprintf("👥 %d reviews", len(i.Reviews))
	}
	
	if desc == "" {
		desc = "Loading PR details..."
	}
	
	return desc
}

// FilterValue implements list.Item
func (i PRItem) FilterValue() string {
	return i.PR.Title
}

// Helper functions

func joinWithCommas(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	if len(items) == 2 {
		return items[0] + " and " + items[1]
	}
	result := ""
	for i, item := range items {
		if i == len(items)-1 {
			result += ", and " + item
		} else if i > 0 {
			result += ", " + item
		} else {
			result += item
		}
	}
	return result
}

func getStatusEmoji(status string) string {
	switch status {
	case "success":
		return "✅ "
	case "failure":
		return "❌ "
	case "pending":
		return "🟡 "
	default:
		return "❓ "
	}
}