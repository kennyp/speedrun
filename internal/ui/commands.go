package ui

import (
	"context"
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
	PRNumber int
	Stats    *github.DiffStats
	Err      error
}

// CheckStatusLoadedMsg is sent when check status has been loaded for a PR
type CheckStatusLoadedMsg struct {
	PRNumber int
	Status   *github.CheckStatus
	Err      error
}

// ReviewsLoadedMsg is sent when reviews have been loaded for a PR
type ReviewsLoadedMsg struct {
	PRNumber int
	Reviews  []*github.Review
	Err      error
}

// AIAnalysisLoadedMsg is sent when AI analysis has been completed for a PR
type AIAnalysisLoadedMsg struct {
	PRNumber int
	Analysis *agent.Analysis
	Err      error
}

// PRApprovedMsg is sent when a PR has been approved
type PRApprovedMsg struct {
	PRNumber int
	Err      error
}

// StatusMsg is a general status update message
type StatusMsg string

// Commands

// FetchPRsCmd fetches PRs from GitHub
func FetchPRsCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		prs, err := client.SearchPullRequests(ctx)
		return PRsLoadedMsg{PRs: prs, Err: err}
	}
}

// FetchDiffStatsCmd fetches diff stats for a PR
func FetchDiffStatsCmd(client *github.Client, pr *github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		stats, err := pr.GetDiffStats(ctx)
		return DiffStatsLoadedMsg{
			PRNumber: pr.Number,
			Stats:    stats,
			Err:      err,
		}
	}
}

// FetchCheckStatusCmd fetches check status for a PR
func FetchCheckStatusCmd(client *github.Client, pr *github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status, err := pr.GetCheckStatus(ctx)
		return CheckStatusLoadedMsg{
			PRNumber: pr.Number,
			Status:   status,
			Err:      err,
		}
	}
}

// FetchReviewsCmd fetches reviews for a PR
func FetchReviewsCmd(client *github.Client, pr *github.PullRequest, username string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reviews, err := pr.GetReviews(ctx)
		return ReviewsLoadedMsg{
			PRNumber: pr.Number,
			Reviews:  reviews,
			Err:      err,
		}
	}
}

// ApprovePRCmd approves a PR
func ApprovePRCmd(pr *github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := pr.Approve(ctx)
		return PRApprovedMsg{
			PRNumber: pr.Number,
			Err:      err,
		}
	}
}

// FetchAIAnalysisCmd runs AI analysis for a PR
func FetchAIAnalysisCmd(aiAgent *agent.Agent, pr *github.PullRequest, diffStats *github.DiffStats, checkStatus *github.CheckStatus, reviews []*github.Review) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Convert github reviews to agent reviews
		var agentReviews []agent.ReviewInfo
		for _, review := range reviews {
			agentReviews = append(agentReviews, agent.ReviewInfo{
				State: review.State,
				User:  review.User,
			})
		}

		// Build PR data
		prData := agent.PRData{
			Title:        pr.Title,
			Number:       pr.Number,
			Additions:    diffStats.Additions,
			Deletions:    diffStats.Deletions,
			ChangedFiles: diffStats.Files,
			CIStatus:     checkStatus.State,
			Reviews:      agentReviews,
		}

		analysis, err := aiAgent.AnalyzePR(ctx, prData)
		return AIAnalysisLoadedMsg{
			PRNumber: pr.Number,
			Analysis: analysis,
			Err:      err,
		}
	}
}

// OpenPRInBrowserCmd opens a PR in the browser
func OpenPRInBrowserCmd(pr *github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		err := pr.OpenInBrowser()
		if err != nil {
			return StatusMsg("Failed to open browser: " + err.Error())
		}
		return StatusMsg("Opened PR in browser")
	}
}