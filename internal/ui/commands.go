package ui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kennyp/speedrun/pkg/agent"
	"github.com/kennyp/speedrun/pkg/github"
)

// Messages

// PRsLoadedMsg is sent when PRs have been loaded from GitHub
type PRsLoadedMsg struct {
	PRs []*github.PullRequest
	Err error
}

// DiffStatsLoadedMsg is sent when diff stats have been loaded for a PR
type DiffStatsLoadedMsg struct {
	PRID  int64
	Stats *github.DiffStats
	Err   error
}

// CheckStatusLoadedMsg is sent when check status has been loaded for a PR
type CheckStatusLoadedMsg struct {
	PRID   int64
	Status *github.CheckStatus
	Err    error
}

// ReviewsLoadedMsg is sent when reviews have been loaded for a PR
type ReviewsLoadedMsg struct {
	PRID    int64
	Reviews []*github.Review
	Err     error
}

// AIAnalysisLoadedMsg is sent when AI analysis has been completed for a PR
type AIAnalysisLoadedMsg struct {
	PRID     int64
	Analysis *agent.Analysis
	Err      error
}

// PRApprovedMsg is sent when a PR has been approved
type PRApprovedMsg struct {
	PRID int64
	Err  error
}

// AutoMergeEnabledMsg is sent when auto-merge has been enabled for a PR
type AutoMergeEnabledMsg struct {
	PRID int64
	Err  error
}

// PRMergedMsg is sent when a PR has been merged directly
type PRMergedMsg struct {
	PRID int64
	Err  error
}

// StatusMsg is a general status update message
type StatusMsg string

// SmartRefreshLoadedMsg is sent when smart refresh has completed
type SmartRefreshLoadedMsg struct {
	PRs []*github.PullRequest
	Err error
}

// Commands

// FetchPRsCmd fetches PRs from GitHub
func FetchPRsCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Starting PR search")
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		prs, err := client.SearchPullRequests(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Error("PR search failed", slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Info("PR search completed", slog.Int("count", len(prs)), slog.Duration("duration", duration))
		}

		return PRsLoadedMsg{PRs: prs, Err: err}
	}
}

// FetchDiffStatsCmd fetches diff stats for a PR
func FetchDiffStatsCmd(client *github.Client, pr *github.PullRequest, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Fetching diff stats", slog.Any("pr", pr))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		stats, err := pr.GetDiffStats(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Debug("Diff stats failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Debug("Diff stats loaded", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("stats", stats))
		}

		return DiffStatsLoadedMsg{
			PRID:  prID,
			Stats: stats,
			Err:   err,
		}
	}
}

// FetchCheckStatusCmd fetches check status for a PR
func FetchCheckStatusCmd(client *github.Client, pr *github.PullRequest, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Fetching check status", slog.Any("pr", pr))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status, err := pr.GetCheckStatus(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Debug("Check status failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Debug("Check status loaded", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("status", status))
		}

		return CheckStatusLoadedMsg{
			PRID:   prID,
			Status: status,
			Err:    err,
		}
	}
}

// FetchReviewsCmd fetches reviews for a PR
func FetchReviewsCmd(client *github.Client, pr *github.PullRequest, username string, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Fetching reviews", slog.Any("pr", pr), slog.String("username", username))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reviews, err := pr.GetReviews(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Debug("Reviews failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			// Check if user has reviewed this PR
			userReviewed := false
			userApproved := false
			for _, review := range reviews {
				if review.User == username {
					userReviewed = true
					if review.State == "APPROVED" {
						userApproved = true
					}
				}
			}
			slog.Debug("Reviews loaded", slog.Any("pr", pr), slog.Duration("duration", duration),
				slog.Int("total_reviews", len(reviews)), slog.Bool("user_reviewed", userReviewed), slog.Bool("user_approved", userApproved))
		}

		return ReviewsLoadedMsg{
			PRID:    prID,
			Reviews: reviews,
			Err:     err,
		}
	}
}

// ApprovePRCmd approves a PR
func ApprovePRCmd(pr *github.PullRequest, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Approving PR", slog.Any("pr", pr))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := pr.Approve(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Error("PR approval failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Info("PR approved successfully", slog.Any("pr", pr), slog.Duration("duration", duration))
		}

		return PRApprovedMsg{
			PRID: prID,
			Err:  err,
		}
	}
}

// EnableAutoMergeCmd enables auto-merge for a PR
func EnableAutoMergeCmd(pr *github.PullRequest, mergeMethod string, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Enabling auto-merge for PR", slog.Any("pr", pr), slog.String("merge_method", mergeMethod))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err := pr.EnableAutoMerge(ctx, mergeMethod)
		duration := time.Since(start)

		if err != nil {
			slog.Error("Auto-merge enabling failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Info("Auto-merge enabled successfully", slog.Any("pr", pr), slog.Duration("duration", duration))
		}

		return AutoMergeEnabledMsg{
			PRID: prID,
			Err:  err,
		}
	}
}

// MergeCmd merges a PR directly
func MergeCmd(pr *github.PullRequest, mergeMethod string, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Merging PR", slog.Any("pr", pr), slog.String("merge_method", mergeMethod))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err := pr.Merge(ctx, mergeMethod)
		duration := time.Since(start)

		if err != nil {
			slog.Error("PR merging failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Info("PR merged successfully", slog.Any("pr", pr), slog.Duration("duration", duration))
		}

		return PRMergedMsg{
			PRID: prID,
			Err:  err,
		}
	}
}

// FetchAIAnalysisCmd runs AI analysis for a PR
func FetchAIAnalysisCmd(aiAgent *agent.Agent, pr *github.PullRequest, diffStats *github.DiffStats, checkStatus *github.CheckStatus, reviews []*github.Review, prID int64, analysisTimeout time.Duration) tea.Cmd {
	return func() tea.Msg {
		// Skip AI analysis if HeadSHA is not yet available
		if pr.HeadSHA == "" {
			slog.Debug("Skipping AI analysis - HeadSHA not available yet", slog.Any("pr", pr))
			return AIAnalysisLoadedMsg{
				PRID:     prID,
				Analysis: nil,
				Err:      fmt.Errorf("HeadSHA not available yet"),
			}
		}

		slog.Debug("Starting AI analysis", slog.Any("pr", pr))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), analysisTimeout)
		defer cancel()

		// Check for cached AI analysis first
		if cachedAnalysis, err := pr.GetCachedAIAnalysis(); err == nil {
			if analysis, ok := cachedAnalysis.(*agent.Analysis); ok {
				duration := time.Since(start)
				slog.Debug("AI analysis loaded from cache", slog.Any("pr", pr), slog.Duration("duration", duration),
					slog.Any("recommendation", analysis.Recommendation), slog.String("risk", analysis.RiskLevel))
				return AIAnalysisLoadedMsg{
					PRID:     prID,
					Analysis: analysis,
					Err:      nil,
				}
			}
		}

		// Convert github reviews to agent reviews
		var agentReviews []agent.ReviewInfo
		for _, review := range reviews {
			agentReviews = append(agentReviews, agent.ReviewInfo{
				State: review.State,
				User:  review.User,
			})
		}

		// Convert check details to agent format
		var checkDetails []agent.CheckInfo
		if checkStatus != nil && checkStatus.Details != nil {
			for _, detail := range checkStatus.Details {
				checkDetails = append(checkDetails, agent.CheckInfo{
					Name:        detail.Name,
					Status:      detail.Status,
					Description: detail.Description,
				})
			}
		}

		// Build PR data
		prData := agent.PRData{
			Title:        pr.Title,
			Number:       pr.Number,
			Additions:    diffStats.Additions,
			Deletions:    diffStats.Deletions,
			ChangedFiles: diffStats.Files,
			CIStatus:     checkStatus.State, // Keep for backward compatibility
			CheckDetails: checkDetails,
			Reviews:      agentReviews,
			HasConflicts: false, // TODO: Fetch merge conflict status
			PRURL:        fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number),
		}

		slog.Debug("Running AI analysis (not cached)", slog.Any("pr", pr))
		analysis, err := aiAgent.AnalyzePR(ctx, prData)
		duration := time.Since(start)

		if err != nil {
			slog.Debug("AI analysis failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Debug("AI analysis completed", slog.Any("pr", pr), slog.Duration("duration", duration),
				slog.Any("recommendation", analysis.Recommendation), slog.String("risk", analysis.RiskLevel))
			// Cache the analysis result
			if err := pr.SetCachedAIAnalysis(analysis); err != nil {
				slog.Debug("Failed to cache AI analysis", slog.Any("pr", pr), slog.Any("error", err))
			}
		}

		return AIAnalysisLoadedMsg{
			PRID:     prID,
			Analysis: analysis,
			Err:      err,
		}
	}
}

// FetchCachedAIAnalysisCmd loads cached AI analysis for a PR
func FetchCachedAIAnalysisCmd(pr *github.PullRequest, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Loading cached AI analysis", slog.Any("pr", pr))

		// Check for cached AI analysis
		if cachedAnalysis, err := pr.GetCachedAIAnalysis(); err == nil {
			if analysis, ok := cachedAnalysis.(*agent.Analysis); ok {
				slog.Debug("Cached AI analysis found", slog.Any("pr", pr),
					slog.Any("recommendation", analysis.Recommendation), slog.String("risk", analysis.RiskLevel))
				return AIAnalysisLoadedMsg{
					PRID:     prID,
					Analysis: analysis,
					Err:      nil,
				}
			}
		}

		// No cached analysis found - this shouldn't happen if we checked properly
		slog.Debug("No cached AI analysis found", slog.Any("pr", pr))
		return nil
	}
}

// TriggerAIAnalysisWhenReadyCmd triggers AI analysis when all prerequisites are met
// This is used in sequential loading to ensure AI analysis happens after HeadSHA is available
func TriggerAIAnalysisWhenReadyCmd(aiAgent *agent.Agent, pr *github.PullRequest, prID int64) tea.Cmd {
	return func() tea.Msg {
		slog.Debug("Checking if AI analysis can be triggered", slog.Any("pr", pr))

		// This command will be processed by the model's Update method
		// which will check if all conditions are met and trigger the actual AI analysis
		return TriggerAIAnalysisMsg{
			PRID: prID,
		}
	}
}

// TriggerAIAnalysisMsg is sent when we want to trigger AI analysis for a PR
type TriggerAIAnalysisMsg struct {
	PRID int64
}

// SmartRefreshCmd fetches fresh PRs for smart refresh
func SmartRefreshCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Starting smart refresh")
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		prs, err := client.SearchPullRequestsFresh(ctx)
		duration := time.Since(start)

		if err != nil {
			slog.Error("Smart refresh failed", slog.Duration("duration", duration), slog.Any("error", err))
		} else {
			slog.Info("Smart refresh completed", slog.Int("count", len(prs)), slog.Duration("duration", duration))
		}

		return SmartRefreshLoadedMsg{PRs: prs, Err: err}
	}
}

// OpenPRInBrowserCmd opens a PR in the browser
func OpenPRInBrowserCmd(pr *github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Opening PR in browser", slog.Any("pr", pr))
		err := pr.OpenInBrowser()
		if err != nil {
			slog.Error("Failed to open browser", slog.Any("pr", pr), slog.Any("error", err))
			return StatusMsg("Failed to open browser: " + err.Error())
		}
		slog.Debug("PR opened in browser", slog.Any("pr", pr))
		return StatusMsg("Opened PR in browser")
	}
}
