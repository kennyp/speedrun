package github

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-github/v73/github"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
	"github.com/kennyp/speedrun/pkg/cache"
)


// ChecksConfig holds CI check filtering configuration
type ChecksConfig struct {
	Ignored  []string // Checks to ignore
	Required []string // If set, only these checks matter
}

// Client wraps the GitHub API client
type Client struct {
	client        *github.Client
	graphqlClient *GraphQLClient
	searchQuery   string
	token         string
	cache         cache.Cache
	backoffConfig backoffconfig.Config
	checksConfig  ChecksConfig
}

// NewClient creates a new GitHub client
func NewClient(ctx context.Context, token, searchQuery string, c cache.Cache, backoffConfig backoffconfig.Config, checksConfig ChecksConfig) (*Client, error) {
	// If no token provided, try to get it from gh CLI
	if token == "" {
		ghToken, err := getGHToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("no GitHub token provided and failed to get from gh CLI: %w", err)
		}
		token = ghToken
	}

	client := github.NewClient(nil).WithAuthToken(token)
	graphqlClient := NewGraphQLClient(token)

	return &Client{
		client:        client,
		graphqlClient: graphqlClient,
		searchQuery:   searchQuery,
		token:         token,
		cache:         c,
		backoffConfig: backoffConfig,
		checksConfig:  checksConfig,
	}, nil
}

// getGHToken gets the GitHub token from the gh CLI
func getGHToken(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// AuthenticatedUser returns the authenticated user's login
func (c *Client) AuthenticatedUser(ctx context.Context) (string, error) {
	slog.Debug("Getting authenticated user")
	start := time.Now()

	user, _, err := c.client.Users.Get(ctx, "")
	duration := time.Since(start)

	if err != nil {
		slog.Error("Failed to get authenticated user", slog.Duration("duration", duration), slog.Any("error", err))
		return "", fmt.Errorf("failed to get authenticated user: %w", err)
	}

	username := user.GetLogin()
	slog.Debug("Successfully retrieved authenticated user", slog.String("username", username), slog.Duration("duration", duration))
	return username, nil
}

// SearchPullRequests searches for pull requests matching the configured query
// cacheKey generates a cache key for search results
func (c *Client) searchCacheKey() string {
	return fmt.Sprintf("search:%s", c.searchQuery)
}

func (c *Client) SearchPullRequests(ctx context.Context) ([]*PullRequest, error) {
	slog.Debug("Starting PR search", slog.String("query", c.searchQuery))
	start := time.Now()

	cacheKey := c.searchCacheKey()

	// Try to get from cache first
	var cachedPRs []*PullRequest
	if err := c.cache.Get(cacheKey, &cachedPRs); err == nil {
			// Restore client field for cached PRs since it's not serialized
			for _, pr := range cachedPRs {
				pr.client = c
			}
			duration := time.Since(start)
			slog.Debug("Retrieved PRs from cache", slog.String("query", c.searchQuery), slog.Int("count", len(cachedPRs)), slog.Duration("duration", duration))
			return cachedPRs, nil
		}

	opts := &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	var result *github.IssuesSearchResult
	operation := func() error {
		var searchErr error
		result, _, searchErr = c.client.Search.Issues(ctx, c.searchQuery, opts)
		return searchErr
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	duration := time.Since(start)

	if err != nil {
		slog.Error("GitHub API search failed", slog.String("query", c.searchQuery), slog.Duration("duration", duration), slog.Any("error", err))
		return nil, fmt.Errorf("failed to search PRs: %w", err)
	}

	slog.Debug("GitHub API search completed", slog.String("query", c.searchQuery), slog.Int("raw_results", len(result.Issues)), slog.Duration("duration", duration))

	var prs []*PullRequest
	for _, issue := range result.Issues {
		// Skip if not a PR
		if issue.PullRequestLinks == nil {
			continue
		}

		// Skip if merged
		if issue.PullRequestLinks.MergedAt != nil {
			continue
		}

		pr, err := newPullRequestFromIssue(c, issue)
		if err != nil {
			slog.Debug("Failed to create PR from issue", slog.String("issue_number", fmt.Sprintf("%d", issue.GetNumber())), slog.Any("error", err))
			continue
		}

		prs = append(prs, pr)
	}

	slog.Info("PR search results processed", slog.String("query", c.searchQuery), slog.Int("filtered_prs", len(prs)), slog.Duration("total_duration", time.Since(start)))

	// Cache the results
	if err := c.cache.Set(cacheKey, prs); err != nil {
		slog.Debug("Failed to cache search results", slog.String("query", c.searchQuery), slog.Any("error", err))
	}

	return prs, nil
}

// SearchPullRequestsFresh searches for pull requests bypassing cache (for refresh)
func (c *Client) SearchPullRequestsFresh(ctx context.Context) ([]*PullRequest, error) {
	slog.Debug("Starting fresh PR search", slog.String("query", c.searchQuery))
	start := time.Now()

	opts := &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	var result *github.IssuesSearchResult
	operation := func() error {
		var searchErr error
		result, _, searchErr = c.client.Search.Issues(ctx, c.searchQuery, opts)
		return searchErr
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	duration := time.Since(start)

	if err != nil {
		slog.Error("GitHub API fresh search failed", slog.String("query", c.searchQuery), slog.Duration("duration", duration), slog.Any("error", err))
		return nil, fmt.Errorf("failed to search PRs: %w", err)
	}

	slog.Debug("GitHub API fresh search completed", slog.String("query", c.searchQuery), slog.Int("raw_results", len(result.Issues)), slog.Duration("duration", duration))

	var prs []*PullRequest
	for _, issue := range result.Issues {
		// Skip if not a PR
		if issue.PullRequestLinks == nil {
			continue
		}

		// Skip if merged
		if issue.PullRequestLinks.MergedAt != nil {
			continue
		}

		pr, err := newPullRequestFromIssue(c, issue)
		if err != nil {
			slog.Debug("Failed to create PR from issue", slog.String("issue_number", fmt.Sprintf("%d", issue.GetNumber())), slog.Any("error", err))
			continue
		}

		prs = append(prs, pr)
	}

	slog.Info("Fresh PR search results processed", slog.String("query", c.searchQuery), slog.Int("filtered_prs", len(prs)), slog.Duration("total_duration", time.Since(start)))

	// Update the cache with fresh results
	cacheKey := c.searchCacheKey()
	if err := c.cache.Set(cacheKey, prs); err != nil {
		slog.Debug("Failed to cache fresh search results", slog.String("query", c.searchQuery), slog.Any("error", err))
	}

	return prs, nil
}

// filterChecks filters check details based on configuration
func (c *Client) filterChecks(details []CheckDetail) []CheckDetail {
	if len(details) == 0 {
		return details
	}

	slog.Debug("Filtering checks",
		slog.Int("total_checks", len(details)),
		slog.Any("ignored_config", c.checksConfig.Ignored),
		slog.Any("required_config", c.checksConfig.Required),
	)

	// If required checks are specified, only keep those
	if len(c.checksConfig.Required) > 0 {
		var filtered []CheckDetail
		requiredMap := make(map[string]bool)
		for _, req := range c.checksConfig.Required {
			requiredMap[req] = true
		}

		for _, detail := range details {
			if requiredMap[detail.Name] {
				filtered = append(filtered, detail)
			}
		}
		slog.Debug("Applied required filter", slog.Int("filtered_count", len(filtered)))
		return filtered
	}

	// Otherwise, filter out ignored checks
	if len(c.checksConfig.Ignored) > 0 {
		var filtered []CheckDetail
		ignoredMap := make(map[string]bool)
		for _, ignored := range c.checksConfig.Ignored {
			ignoredMap[ignored] = true
		}

		for _, detail := range details {
			isIgnored := ignoredMap[detail.Name]
			slog.Debug("Check filtering",
				slog.String("check_name", detail.Name),
				slog.String("check_status", detail.Status),
				slog.Bool("is_ignored", isIgnored),
			)
			if !isIgnored {
				filtered = append(filtered, detail)
			}
		}
		slog.Debug("Applied ignored filter",
			slog.Int("original_count", len(details)),
			slog.Int("filtered_count", len(filtered)),
		)
		return filtered
	}

	// No filtering configured, return all
	slog.Debug("No filtering configured, returning all checks")
	return details
}

// EnableAutoMerge enables auto-merge for a pull request
func (c *Client) EnableAutoMerge(ctx context.Context, owner, repo string, number int, mergeMethod string) error {
	slog.Debug("Enabling auto-merge for PR", "owner", owner, "repo", repo, "number", number, "merge_method", mergeMethod)

	// Get the GraphQL node ID for the pull request
	nodeID, err := c.graphqlClient.GetPullRequestNodeID(ctx, owner, repo, number)
	if err != nil {
		return fmt.Errorf("failed to get PR node ID: %w", err)
	}

	// Enable auto-merge using GraphQL
	_, err = c.graphqlClient.EnableAutoMerge(ctx, nodeID, mergeMethod)
	if err != nil {
		return fmt.Errorf("failed to enable auto-merge: %w", err)
	}

	slog.Info("Auto-merge enabled successfully", "owner", owner, "repo", repo, "number", number, "merge_method", mergeMethod)
	return nil
}

// Merge merges a pull request immediately using the REST API
func (c *Client) Merge(ctx context.Context, owner, repo string, number int, mergeMethod string) error {
	slog.Debug("Merging PR", "owner", owner, "repo", repo, "number", number, "merge_method", mergeMethod)

	// Convert merge method to REST API format
	restMergeMethod := strings.ToLower(mergeMethod)
	if restMergeMethod == "" {
		restMergeMethod = "squash"
	}

	mergeOptions := &github.PullRequestOptions{
		MergeMethod: restMergeMethod,
		CommitTitle: "", // Let GitHub generate the title
	}

	result, _, err := c.client.PullRequests.Merge(ctx, owner, repo, number, "", mergeOptions)
	if err != nil {
		return fmt.Errorf("failed to merge PR: %w", err)
	}

	if !result.GetMerged() {
		return fmt.Errorf("PR was not merged - %s", result.GetMessage())
	}

	slog.Info("PR merged successfully", "owner", owner, "repo", repo, "number", number, "merge_method", mergeMethod, "sha", result.GetSHA())
	return nil
}

// GetPRDetails gets detailed information about a pull request
func (c *Client) GetPRDetails(ctx context.Context, owner, repo string, number int) (string, error) {
	var pr *github.PullRequest
	operation := func() error {
		var err error
		pr, _, err = c.client.PullRequests.Get(ctx, owner, repo, number)
		return err
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
		return "", fmt.Errorf("failed to get PR details: %w", err)
	}

	// Return formatted PR details
	details := fmt.Sprintf("PR #%d: %s\n", pr.GetNumber(), pr.GetTitle())
	details += fmt.Sprintf("State: %s\n", pr.GetState())
	details += fmt.Sprintf("Author: %s\n", pr.GetUser().GetLogin())
	details += fmt.Sprintf("Additions: %d, Deletions: %d, Changed Files: %d\n", 
		pr.GetAdditions(), pr.GetDeletions(), pr.GetChangedFiles())
	details += fmt.Sprintf("Mergeable: %v\n", pr.GetMergeable())
	if pr.GetBody() != "" {
		details += fmt.Sprintf("Description: %s\n", pr.GetBody())
	}
	return details, nil
}

// GetPRDiff gets the diff for a pull request
func (c *Client) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	var diff string
	operation := func() error {
		var err error
		diff, _, err = c.client.PullRequests.GetRaw(ctx, owner, repo, number, github.RawOptions{
			Type: github.Diff,
		})
		return err
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
		return "", fmt.Errorf("failed to get PR diff: %w", err)
	}

	// Truncate very large diffs to avoid overwhelming the model
	if len(diff) > 8000 {
		diff = diff[:8000] + "\n... (diff truncated due to size)"
	}
	return diff, nil
}

// GetFileContent gets the content of a file from a repository
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD" // Default to HEAD if no ref specified
	}

	var content *github.RepositoryContent
	operation := func() error {
		var err error
		content, _, _, err = c.client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{
			Ref: ref,
		})
		return err
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	fileContent, err := content.GetContent()
	if err != nil {
		return "", fmt.Errorf("failed to decode file content: %w", err)
	}

	// Truncate very large files
	if len(fileContent) > 5000 {
		fileContent = fileContent[:5000] + "\n... (file truncated due to size)"
	}
	return fileContent, nil
}

// GetPRComments gets comments for a pull request
func (c *Client) GetPRComments(ctx context.Context, owner, repo string, number int) (string, error) {
	var comments []*github.PullRequestComment
	operation := func() error {
		var err error
		comments, _, err = c.client.PullRequests.ListComments(ctx, owner, repo, number, nil)
		return err
	}

	exponentialBackoff := c.backoffConfig.ToExponentialBackoff()
	if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
		return "", fmt.Errorf("failed to get PR comments: %w", err)
	}

	if len(comments) == 0 {
		return "No comments found on this PR.", nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d comments:\n\n", len(comments)))

	for i, comment := range comments {
		if i >= 10 { // Limit to first 10 comments
			result.WriteString("... (remaining comments truncated)\n")
			break
		}
		result.WriteString(fmt.Sprintf("Comment by %s:\n%s\n\n", 
			comment.GetUser().GetLogin(), comment.GetBody()))
	}

	return result.String(), nil
}
