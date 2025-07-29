package ui

import (
	"fmt"

	"github.com/kennyp/speedrun/pkg/agent"
	"github.com/kennyp/speedrun/pkg/github"
)

// PRItem represents a PR in the list
type PRItem struct {
	ID          int64 // Unique atomic ID for this PR item
	PR          *github.PullRequest
	DiffStats   *github.DiffStats
	CheckStatus *github.CheckStatus
	Reviews     []*github.Review
	AIAnalysis  *agent.Analysis

	// Loading states
	LoadingDiff    bool
	LoadingChecks  bool
	LoadingReviews bool
	LoadingAI      bool

	// Completion states
	Approved  bool
	Reviewed  bool // Has the current user reviewed this PR?
	Dismissed bool // Has the current user's review been dismissed?

	// Errors
	DiffError   error
	CheckError  error
	ReviewError error
	AIError     error
}

// Title implements list.Item
func (i PRItem) Title() string {
	status := "📊"
	if i.Approved {
		status = "✅"
	} else if i.Dismissed {
		status = "⚠️" // Warning for dismissed reviews
	} else if i.Reviewed {
		status = "👀"
	} else if i.AIAnalysis != nil {
		status = getRecommendationEmoji(i.AIAnalysis.Recommendation)
	}
	
	// Add PR type indicator to title for special types
	title := fmt.Sprintf("%s PR #%d: %s", status, i.PR.Number, i.PR.Title)
	if i.AIAnalysis != nil && i.AIAnalysis.PRType != "" && i.AIAnalysis.PRType != "CODE" {
		typeEmoji := getPRTypeEmoji(i.AIAnalysis.PRType)
		title = fmt.Sprintf("%s %s PR #%d: %s", status, typeEmoji, i.PR.Number, i.PR.Title)
	}
	
	return title
}

// Description implements list.Item
func (i PRItem) Description() string {
	// Build description from available data immediately
	desc := ""

	// Diff stats
	if i.DiffStats != nil {
		desc += fmt.Sprintf("📊 +%d/-%d lines, %d files",
			i.DiffStats.Additions, i.DiffStats.Deletions, i.DiffStats.Files)
	} else if i.LoadingDiff {
		desc += "📊 Loading diff..."
	} else if i.DiffError != nil {
		desc += "📊 ⚠️ Diff error"
	}

	// Check status
	if i.CheckStatus != nil {
		if desc != "" {
			desc += " | "
		}
		emoji := getStatusEmoji(i.CheckStatus.State)
		desc += fmt.Sprintf("🔧 %s%s", emoji, i.CheckStatus.Description)
	} else if i.LoadingChecks {
		if desc != "" {
			desc += " | "
		}
		desc += "🔧 Loading checks..."
	} else if i.CheckError != nil {
		if desc != "" {
			desc += " | "
		}
		desc += "🔧 ⚠️ Check error"
	}

	// Reviews
	if len(i.Reviews) > 0 {
		if desc != "" {
			desc += " | "
		}
		desc += fmt.Sprintf("👥 %d reviews", len(i.Reviews))
	} else if i.LoadingReviews {
		if desc != "" {
			desc += " | "
		}
		desc += "👥 Loading reviews..."
	} else if i.ReviewError != nil {
		if desc != "" {
			desc += " | "
		}
		desc += "👥 ⚠️ Review error"
	}

	// AI Analysis
	if i.AIAnalysis != nil {
		if desc != "" {
			desc += " | "
		}
		emoji := getRecommendationEmoji(i.AIAnalysis.Recommendation)
		riskEmoji := getRiskEmoji(i.AIAnalysis.RiskLevel)
		typeEmoji := getPRTypeEmoji(i.AIAnalysis.PRType)
		
		// Build AI analysis string
		aiDesc := fmt.Sprintf("🤖 %s %s (%s %s Risk)", emoji, i.AIAnalysis.Recommendation, riskEmoji, i.AIAnalysis.RiskLevel)
		
		// Add PR type if available
		if i.AIAnalysis.PRType != "" {
			aiDesc += fmt.Sprintf(" | %s %s", typeEmoji, i.AIAnalysis.PRType)
			// Add doc type for documentation PRs
			if i.AIAnalysis.PRType == "DOCUMENTATION" && i.AIAnalysis.DocType != "" {
				aiDesc += fmt.Sprintf(" (%s)", i.AIAnalysis.DocType)
			}
		}
		
		desc += aiDesc
	} else if i.LoadingAI {
		if desc != "" {
			desc += " | "
		}
		desc += "🤖 AI analyzing..."
	} else if i.AIError != nil {
		if desc != "" {
			desc += " | "
		}
		desc += "🤖 ⚠️ AI error"
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

func getRecommendationEmoji(recommendation agent.Recommendation) string {
	switch recommendation {
	case agent.Approve:
		return "✅"
	case agent.Review:
		return "👀"
	case agent.DeepReview:
		return "🔍"
	default:
		return "❓"
	}
}

func getRiskEmoji(riskLevel string) string {
	switch riskLevel {
	case "LOW":
		return "🟢"
	case "MEDIUM":
		return "🟡"
	case "HIGH":
		return "🔴"
	default:
		return "⚪"
	}
}

func getPRTypeEmoji(prType string) string {
	switch prType {
	case "DOCUMENTATION":
		return "📝"
	case "CODE":
		return "💻"
	case "DEPENDENCY":
		return "📦"
	case "MIXED":
		return "🔀"
	default:
		return "❓"
	}
}
