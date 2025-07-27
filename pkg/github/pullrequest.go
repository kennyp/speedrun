package github

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/google/go-github/v73/github"
)

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Number    int
	Title     string
	Owner     string
	Repo      string
	URL       *url.URL
	UpdatedAt time.Time
	HeadSHA   string

	client *Client
	ghi    *github.Issue
}

// newPullRequestFromIssue creates a PullRequest from a GitHub Issue
func newPullRequestFromIssue(client *Client, issue *github.Issue) (*PullRequest, error) {
	pr := &PullRequest{
		Number:    issue.GetNumber(),
		Title:     issue.GetTitle(),
		UpdatedAt: issue.GetUpdatedAt().Time,
		client:    client,
		ghi:       issue,
	}

	// Parse the URL to extract owner and repo
	issueURL, err := url.Parse(issue.GetURL())
	if err != nil {
		return nil, fmt.Errorf("failed to parse issue URL: %w", err)
	}
	pr.URL = issueURL

	// Extract owner and repo from URL path
	// URL format: https://api.github.com/repos/OWNER/REPO/issues/NUMBER
	parts := strings.Split(issueURL.Path, "/")
	if len(parts) < 5 {
		return nil, fmt.Errorf("unexpected URL format: %s", issueURL.Path)
	}
	pr.Owner = parts[2]
	pr.Repo = parts[3]

	return pr, nil
}

// GetReviews returns the reviews for this PR
func (pr *PullRequest) GetReviews(ctx context.Context) ([]*Review, error) {
	reviews, _, err := pr.client.client.PullRequests.ListReviews(ctx, pr.Owner, pr.Repo, pr.Number, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get reviews: %w", err)
	}

	var result []*Review
	for _, review := range reviews {
		result = append(result, &Review{
			State: review.GetState(),
			User:  review.GetUser().GetLogin(),
			Body:  review.GetBody(),
		})
	}
	return result, nil
}

// HasUserReviewed checks if a specific user has reviewed this PR
func (pr *PullRequest) HasUserReviewed(ctx context.Context, username string) (bool, error) {
	reviews, err := pr.GetReviews(ctx)
	if err != nil {
		return false, err
	}

	for _, review := range reviews {
		if review.User == username {
			return true, nil
		}
	}
	return false, nil
}

// GetCheckStatus returns the combined check status for this PR
func (pr *PullRequest) GetCheckStatus(ctx context.Context) (*CheckStatus, error) {
	// Get the PR details first to get the head SHA
	prDetails, _, err := pr.client.client.PullRequests.Get(ctx, pr.Owner, pr.Repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR details: %w", err)
	}

	pr.HeadSHA = prDetails.GetHead().GetSHA()

	// Get both check runs (modern) and statuses (legacy)
	checkRuns, _, _ := pr.client.client.Checks.ListCheckRunsForRef(ctx, pr.Owner, pr.Repo, pr.HeadSHA, nil)
	statuses, _, _ := pr.client.client.Repositories.GetCombinedStatus(ctx, pr.Owner, pr.Repo, pr.HeadSHA, nil)

	status := &CheckStatus{
		Details: make([]CheckDetail, 0),
	}

	// Process check runs
	if checkRuns != nil {
		for _, run := range checkRuns.CheckRuns {
			detail := CheckDetail{
				Name:        run.GetName(),
				Status:      convertCheckRunStatus(run.GetStatus(), run.GetConclusion()),
				Description: run.GetOutput().GetSummary(),
				URL:         run.GetHTMLURL(),
			}
			status.Details = append(status.Details, detail)
		}
	}

	// Process legacy statuses
	if statuses != nil {
		for _, s := range statuses.Statuses {
			detail := CheckDetail{
				Name:        s.GetContext(),
				Status:      s.GetState(),
				Description: s.GetDescription(),
				URL:         s.GetTargetURL(),
			}
			status.Details = append(status.Details, detail)
		}
	}

	// Determine overall status
	status.State = aggregateCheckStates(status.Details)
	status.Description = formatCheckDescription(status.Details)

	return status, nil
}

// GetDiffStats returns the diff statistics for this PR
func (pr *PullRequest) GetDiffStats(ctx context.Context) (*DiffStats, error) {
	prDetails, _, err := pr.client.client.PullRequests.Get(ctx, pr.Owner, pr.Repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR details: %w", err)
	}

	return &DiffStats{
		Additions: prDetails.GetAdditions(),
		Deletions: prDetails.GetDeletions(),
		Changes:   prDetails.GetChangedFiles(),
		Files:     prDetails.GetChangedFiles(),
	}, nil
}

// Approve approves this PR
func (pr *PullRequest) Approve(ctx context.Context) error {
	review := &github.PullRequestReviewRequest{
		Event: github.String("APPROVE"),
		Body:  github.String("LGTM"),
	}

	_, _, err := pr.client.client.PullRequests.CreateReview(ctx, pr.Owner, pr.Repo, pr.Number, review)
	if err != nil {
		return fmt.Errorf("failed to approve PR: %w", err)
	}

	return nil
}

// OpenInBrowser opens this PR in the default web browser
func (pr *PullRequest) OpenInBrowser() error {
	htmlURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
	cmd := exec.Command("open", htmlURL)
	return cmd.Run()
}

// convertCheckRunStatus converts GitHub check run status/conclusion to simplified state
func convertCheckRunStatus(status, conclusion string) string {
	if status == "completed" {
		switch conclusion {
		case "success":
			return "success"
		case "failure", "cancelled", "timed_out":
			return "failure"
		case "neutral", "skipped":
			return "success" // Treat neutral/skipped as success
		default:
			return "error"
		}
	}
	// Status is queued, in_progress, etc.
	return "pending"
}

// aggregateCheckStates determines overall state from individual check states
func aggregateCheckStates(details []CheckDetail) string {
	if len(details) == 0 {
		return "pending"
	}

	hasFailure := false
	hasPending := false

	for _, detail := range details {
		switch detail.Status {
		case "failure", "error":
			hasFailure = true
		case "pending", "in_progress":
			hasPending = true
		}
	}

	if hasFailure {
		return "failure"
	}
	if hasPending {
		return "pending"
	}
	return "success"
}

// formatCheckDescription creates a human-readable description of check status
func formatCheckDescription(details []CheckDetail) string {
	if len(details) == 0 {
		return "No checks found"
	}

	successCount := 0
	failureCount := 0
	pendingCount := 0

	for _, detail := range details {
		switch detail.Status {
		case "success":
			successCount++
		case "failure", "error":
			failureCount++
		case "pending", "in_progress":
			pendingCount++
		}
	}

	return fmt.Sprintf("%d checks: %d passing, %d failing, %d pending",
		len(details), successCount, failureCount, pendingCount)
}