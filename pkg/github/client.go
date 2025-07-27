package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/google/go-github/v73/github"
)

// Client wraps the GitHub API client
type Client struct {
	client      *github.Client
	searchQuery string
	token       string
}

// NewClient creates a new GitHub client
func NewClient(ctx context.Context, token, searchQuery string) (*Client, error) {
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
		client:      client,
		searchQuery: searchQuery,
		token:       token,
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
func (c *Client) SearchPullRequests(ctx context.Context) ([]*PullRequest, error) {
	opts := &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	result, _, err := c.client.Search.Issues(ctx, c.searchQuery, opts)
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

	return prs, nil
}