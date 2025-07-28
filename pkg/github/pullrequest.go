package github

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
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

// LogValue implements slog.LogValuer for structured logging
func (pr *PullRequest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("number", pr.Number),
		slog.String("repo", fmt.Sprintf("%s/%s", pr.Owner, pr.Repo)),
		slog.String("title", pr.Title),
		slog.String("head_sha", pr.HeadSHA),
		slog.Time("updated_at", pr.UpdatedAt),
	)
}

// Cache key helpers for PR data
func (pr *PullRequest) diffStatsCacheKey() string {
	return fmt.Sprintf("diff:%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
}

func (pr *PullRequest) checkStatusCacheKey() string {
	return fmt.Sprintf("checks:%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
}

func (pr *PullRequest) reviewsCacheKey() string {
	return fmt.Sprintf("reviews:%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
}

func (pr *PullRequest) aiAnalysisCacheKey() string {
	return fmt.Sprintf("ai:%s/%s#%d:%s", pr.Owner, pr.Repo, pr.Number, pr.HeadSHA)
}

// invalidateCache removes all cached data for this PR
func (pr *PullRequest) invalidateCache() {

	// Delete all cached data for this PR
	if err := pr.client.cache.Delete(pr.diffStatsCacheKey()); err != nil {
		slog.Debug("Failed to delete diff stats cache", slog.Any("error", err))
	}
	if err := pr.client.cache.Delete(pr.checkStatusCacheKey()); err != nil {
		slog.Debug("Failed to delete check status cache", slog.Any("error", err))
	}
	if err := pr.client.cache.Delete(pr.reviewsCacheKey()); err != nil {
		slog.Debug("Failed to delete reviews cache", slog.Any("error", err))
	}
	if err := pr.client.cache.Delete(pr.aiAnalysisCacheKey()); err != nil {
		slog.Debug("Failed to delete AI analysis cache", slog.Any("error", err))
	}
}

// InvalidateCommitRelatedCache removes cached data that changes when commits are updated
func (pr *PullRequest) InvalidateCommitRelatedCache() {

	// Delete commit-related cached data (but preserve reviews)
	// Note: AI analysis cache is not deleted here since it uses HeadSHA in the key
	// and will naturally miss when the commit changes
	if err := pr.client.cache.Delete(pr.diffStatsCacheKey()); err != nil {
		slog.Debug("Failed to delete diff stats cache", slog.Any("error", err))
	}
	if err := pr.client.cache.Delete(pr.checkStatusCacheKey()); err != nil {
		slog.Debug("Failed to delete check status cache", slog.Any("error", err))
	}
}

// GetCachedAIAnalysis retrieves cached AI analysis for this PR
func (pr *PullRequest) GetCachedAIAnalysis() (any, error) {

	var cachedAnalysis any
	cacheKey := pr.aiAnalysisCacheKey()
	if err := pr.client.cache.Get(cacheKey, &cachedAnalysis); err != nil {
		return nil, err
	}

	// Validate cached AI analysis - if it's nil, delete the bad cache entry
	if cachedAnalysis == nil {
		slog.Debug("Deleting invalid cached AI analysis (nil)", slog.Any("pr", pr))
		if err := pr.client.cache.Delete(cacheKey); err != nil {
			slog.Debug("Failed to delete invalid AI analysis cache", slog.Any("error", err))
		}
		return nil, fmt.Errorf("cached AI analysis was invalid")
	}

	return cachedAnalysis, nil
}

// SetCachedAIAnalysis stores AI analysis in cache for this PR
func (pr *PullRequest) SetCachedAIAnalysis(analysis any) error {

	// Only cache valid AI analysis (not nil)
	if analysis == nil {
		slog.Debug("Skipping cache of invalid AI analysis (nil)", slog.Any("pr", pr))
		return fmt.Errorf("cannot cache nil AI analysis")
	}

	cacheKey := pr.aiAnalysisCacheKey()
	return pr.client.cache.Set(cacheKey, analysis)
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
	if pr.client == nil {
		return nil, fmt.Errorf("PR client is nil")
	}

	slog.Debug("Getting PR reviews", slog.Any("pr", pr))
	start := time.Now()

	cacheKey := pr.reviewsCacheKey()

	// Try to get from cache first
	var cachedReviews []*Review
	if err := pr.client.cache.Get(cacheKey, &cachedReviews); err == nil {
			// Validate cached data - if it's nil, delete the bad cache entry and fetch fresh
			if cachedReviews != nil {
				duration := time.Since(start)
				slog.Debug("Retrieved reviews from cache", slog.Any("pr", pr), slog.Int("count", len(cachedReviews)), slog.Duration("duration", duration))
				return cachedReviews, nil
			} else {
				// Bad cached data (nil) - delete it and fetch fresh
				slog.Debug("Deleting invalid cached reviews (nil)", slog.Any("pr", pr))
				if err := pr.client.cache.Delete(cacheKey); err != nil {
					slog.Debug("Failed to delete invalid reviews cache", slog.Any("error", err))
				}
				// Fall through to fresh API call
			}
		}

	var reviews []*github.PullRequestReview
	operation := func() error {
		var reviewErr error
		reviews, _, reviewErr = pr.client.client.PullRequests.ListReviews(ctx, pr.Owner, pr.Repo, pr.Number, nil)
		return reviewErr
	}

	exponentialBackoff := pr.client.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	duration := time.Since(start)

	if err != nil {
		slog.Error("GitHub API get reviews failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		return nil, fmt.Errorf("failed to get reviews: %w", err)
	}

	result := make([]*Review, 0)
	for _, review := range reviews {
		result = append(result, &Review{
			State: review.GetState(),
			User:  review.GetUser().GetLogin(),
			Body:  review.GetBody(),
		})
	}

	slog.Debug("GitHub API get reviews completed", slog.Any("pr", pr), slog.Int("count", len(result)), slog.Duration("duration", time.Since(start)))

	// Cache the results - only cache valid reviews (not nil)
	if result != nil {
		if err := pr.client.cache.Set(cacheKey, result); err != nil {
			slog.Debug("Failed to cache reviews", slog.Any("error", err))
		}
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
	if pr.client == nil {
		return nil, fmt.Errorf("PR client is nil")
	}

	slog.Debug("Getting PR check status", slog.Any("pr", pr))
	start := time.Now()

	cacheKey := pr.checkStatusCacheKey()

	// Try to get from cache first
	var cachedStatus *CheckStatus
	if err := pr.client.cache.Get(cacheKey, &cachedStatus); err == nil {
			// Validate cached data - if it's nil or has invalid state, delete and fetch fresh
			if cachedStatus != nil && cachedStatus.State != "" && cachedStatus.Description != "" {
				duration := time.Since(start)
				slog.Debug("Retrieved check status from cache", slog.Any("pr", pr), slog.Any("status", cachedStatus), slog.Duration("duration", duration))

				// If HeadSHA is not populated, we still need to fetch PR details to get it
				if pr.HeadSHA == "" {
					slog.Debug("HeadSHA not available, fetching PR details", slog.Any("pr", pr))
					var prDetails *github.PullRequest
					operation := func() error {
						var getErr error
						prDetails, _, getErr = pr.client.client.PullRequests.Get(ctx, pr.Owner, pr.Repo, pr.Number)
						return getErr
					}

					exponentialBackoff := pr.client.backoffConfig.ToExponentialBackoff()
					if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err == nil {
						pr.HeadSHA = prDetails.GetHead().GetSHA()
						slog.Debug("Retrieved PR details for HeadSHA", slog.Any("pr", pr), slog.String("head_sha", pr.HeadSHA))
					} else {
						slog.Debug("Failed to get PR details for HeadSHA", slog.Any("pr", pr), slog.Any("error", err))
					}
				}

				return cachedStatus, nil
			} else {
				// Bad cached data (nil or invalid state/description) - delete it and fetch fresh
				slog.Debug("Deleting invalid cached check status", slog.Any("pr", pr), slog.Any("status", cachedStatus))
				if err := pr.client.cache.Delete(cacheKey); err != nil {
					slog.Debug("Failed to delete invalid check status cache", slog.Any("error", err))
				}
				// Fall through to fresh API call
			}
		}

	// Get the PR details first to get the head SHA
	var prDetails *github.PullRequest
	operation := func() error {
		var getErr error
		prDetails, _, getErr = pr.client.client.PullRequests.Get(ctx, pr.Owner, pr.Repo, pr.Number)
		return getErr
	}

	exponentialBackoff := pr.client.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	if err != nil {
		duration := time.Since(start)
		slog.Error("GitHub API get PR details failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		return nil, fmt.Errorf("failed to get PR details: %w", err)
	}

	pr.HeadSHA = prDetails.GetHead().GetSHA()
	slog.Debug("Retrieved PR details", slog.Any("pr", pr), slog.String("head_sha", pr.HeadSHA))

	// Get both check runs (modern) and statuses (legacy)
	var checkRuns *github.ListCheckRunsResults
	var statuses *github.CombinedStatus

	// Get check runs with retry
	checkOperation := func() error {
		var checkErr error
		checkRuns, _, checkErr = pr.client.client.Checks.ListCheckRunsForRef(ctx, pr.Owner, pr.Repo, pr.HeadSHA, nil)
		return checkErr
	}
	if err := backoff.Retry(checkOperation, backoff.WithContext(pr.client.backoffConfig.ToExponentialBackoff(), ctx)); err != nil {
		slog.Debug("Failed to get check runs after retries", slog.Any("error", err))
	}

	// Get statuses with retry
	statusOperation := func() error {
		var statusErr error
		statuses, _, statusErr = pr.client.client.Repositories.GetCombinedStatus(ctx, pr.Owner, pr.Repo, pr.HeadSHA, nil)
		return statusErr
	}
	if err := backoff.Retry(statusOperation, backoff.WithContext(pr.client.backoffConfig.ToExponentialBackoff(), ctx)); err != nil {
		slog.Debug("Failed to get combined status after retries", slog.Any("error", err))
	}

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

	// Apply check filtering based on configuration
	filteredDetails := pr.client.filterChecks(status.Details)

	// Determine overall status
	status.State = aggregateCheckStates(filteredDetails)
	status.Description = formatCheckDescription(filteredDetails)
	status.Details = filteredDetails

	slog.Debug("GitHub API get check status completed", slog.Any("pr", pr), slog.Any("status", status), slog.Duration("duration", time.Since(start)))

	// Cache the results - only cache valid status (not nil and has state/description)
	if status != nil && status.State != "" && status.Description != "" {
		if err := pr.client.cache.Set(cacheKey, status); err != nil {
			slog.Debug("Failed to cache check status", slog.Any("error", err))
		}
	}

	return status, nil
}

// GetDiffStats returns the diff statistics for this PR
func (pr *PullRequest) GetDiffStats(ctx context.Context) (*DiffStats, error) {
	if pr.client == nil {
		return nil, fmt.Errorf("PR client is nil")
	}

	slog.Debug("Getting PR diff stats", slog.Any("pr", pr))
	start := time.Now()

	cacheKey := pr.diffStatsCacheKey()

	// Try to get from cache first
	var cachedStats *DiffStats
	if err := pr.client.cache.Get(cacheKey, &cachedStats); err == nil {
			// Validate cached data - if it's nil or has invalid values, delete and fetch fresh
			if cachedStats != nil && cachedStats.Additions >= 0 && cachedStats.Deletions >= 0 && cachedStats.Files >= 0 {
				duration := time.Since(start)
				slog.Debug("Retrieved diff stats from cache", slog.Any("pr", pr), slog.Any("stats", cachedStats), slog.Duration("duration", duration))
				return cachedStats, nil
			} else {
				// Bad cached data (nil or invalid values) - delete it and fetch fresh
				slog.Debug("Deleting invalid cached diff stats", slog.Any("pr", pr), slog.Any("stats", cachedStats))
				if err := pr.client.cache.Delete(cacheKey); err != nil {
					slog.Debug("Failed to delete invalid diff stats cache", slog.Any("error", err))
				}
				// Fall through to fresh API call
			}
		}

	var prDetails *github.PullRequest
	operation := func() error {
		var getErr error
		prDetails, _, getErr = pr.client.client.PullRequests.Get(ctx, pr.Owner, pr.Repo, pr.Number)
		return getErr
	}

	exponentialBackoff := pr.client.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	duration := time.Since(start)

	if err != nil {
		slog.Error("GitHub API get diff stats failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		return nil, fmt.Errorf("failed to get PR details: %w", err)
	}

	stats := &DiffStats{
		Additions: prDetails.GetAdditions(),
		Deletions: prDetails.GetDeletions(),
		Changes:   prDetails.GetChangedFiles(),
		Files:     prDetails.GetChangedFiles(),
	}

	slog.Debug("GitHub API get diff stats completed", slog.Any("pr", pr), slog.Any("stats", stats), slog.Duration("duration", time.Since(start)))

	// Cache the results - only cache valid stats (not nil and has non-negative values)
	if stats != nil && stats.Additions >= 0 && stats.Deletions >= 0 && stats.Files >= 0 {
		if err := pr.client.cache.Set(cacheKey, stats); err != nil {
			slog.Debug("Failed to cache diff stats", slog.Any("error", err))
		}
	}

	return stats, nil
}

// Approve approves this PR
func (pr *PullRequest) Approve(ctx context.Context) error {
	slog.Debug("Approving PR", slog.Any("pr", pr))
	start := time.Now()

	review := &github.PullRequestReviewRequest{
		Event: github.Ptr("APPROVE"),
		Body:  github.Ptr("LGTM"),
	}

	_, _, err := pr.client.client.PullRequests.CreateReview(ctx, pr.Owner, pr.Repo, pr.Number, review)
	duration := time.Since(start)

	if err != nil {
		slog.Error("GitHub API approve PR failed", slog.Any("pr", pr), slog.Duration("duration", duration), slog.Any("error", err))
		return fmt.Errorf("failed to approve PR: %w", err)
	}

	slog.Info("GitHub API approve PR completed", slog.Any("pr", pr), slog.Duration("duration", duration))

	// Invalidate cache since PR state has changed
	pr.invalidateCache()

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

// EnableAutoMerge enables auto-merge for this pull request
func (pr *PullRequest) EnableAutoMerge(ctx context.Context, mergeMethod string) error {
	slog.Debug("Enabling auto-merge for PR", slog.Any("pr", pr), slog.String("merge_method", mergeMethod))

	return pr.client.EnableAutoMerge(ctx, pr.Owner, pr.Repo, pr.Number, mergeMethod)
}

// Merge merges this pull request immediately
func (pr *PullRequest) Merge(ctx context.Context, mergeMethod string) error {
	slog.Debug("Merging PR", slog.Any("pr", pr), slog.String("merge_method", mergeMethod))

	return pr.client.Merge(ctx, pr.Owner, pr.Repo, pr.Number, mergeMethod)
}
