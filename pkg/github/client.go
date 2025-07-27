package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/go-github/v73/github"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
)

// Cache interface for GitHub data caching
type Cache interface {
	Get(key string, dest interface{}) error
	Set(key string, value interface{}) error
	Delete(key string) error
}

// ChecksConfig holds CI check filtering configuration
type ChecksConfig struct {
	Ignored  []string // Checks to ignore
	Required []string // If set, only these checks matter
}

// Client wraps the GitHub API client
type Client struct {
	client        *github.Client
	searchQuery   string
	token         string
	cache         Cache
	backoffConfig backoffconfig.Config
	checksConfig  ChecksConfig
}

// NewClient creates a new GitHub client
func NewClient(ctx context.Context, token, searchQuery string, cache Cache, backoffConfig backoffconfig.Config, checksConfig ChecksConfig) (*Client, error) {
	// If no token provided, try to get it from gh CLI
	if token == "" {
		ghToken, err := getGHToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("no GitHub token provided and failed to get from gh CLI: %w", err)
		}
		token = ghToken
	}

	client := github.NewClient(nil).WithAuthToken(token)
	
	return &Client{
		client:        client,
		searchQuery:   searchQuery,
		token:         token,
		cache:         cache,
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
	user, _, err := c.client.Users.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("failed to get authenticated user: %w", err)
	}
	return user.GetLogin(), nil
}

// SearchPullRequests searches for pull requests matching the configured query
// cacheKey generates a cache key for search results
func (c *Client) searchCacheKey() string {
	return fmt.Sprintf("search:%s", c.searchQuery)
}

func (c *Client) SearchPullRequests(ctx context.Context) ([]*PullRequest, error) {
	cacheKey := c.searchCacheKey()
	
	// Try to get from cache first
	if c.cache != nil {
		var cachedPRs []*PullRequest
		if err := c.cache.Get(cacheKey, &cachedPRs); err == nil {
			return cachedPRs, nil
		}
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
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs: %w", err)
	}

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
			// Log error but continue with other PRs
			continue
		}

		prs = append(prs, pr)
	}

	// Cache the results
	if c.cache != nil {
		c.cache.Set(cacheKey, prs)
	}

	return prs, nil
}

// filterChecks filters check details based on configuration
func (c *Client) filterChecks(details []CheckDetail) []CheckDetail {
	if len(details) == 0 {
		return details
	}

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
			if !ignoredMap[detail.Name] {
				filtered = append(filtered, detail)
			}
		}
		return filtered
	}

	// No filtering configured, return all
	return details
}