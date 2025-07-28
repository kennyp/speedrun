package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// GraphQLClient handles GitHub GraphQL API requests for specific operations
// that are not available in the REST API (like auto-merge)
type GraphQLClient struct {
	token      string
	httpClient *http.Client
}

// NewGraphQLClient creates a new GraphQL client
func NewGraphQLClient(token string) *GraphQLClient {
	return &GraphQLClient{
		token:      token,
		httpClient: &http.Client{},
	}
}

// GraphQLResponse represents a GitHub GraphQL API response
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message   string                 `json:"message"`
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	Path      []interface{}          `json:"path,omitempty"`
}

// GraphQLErrorLocation represents the location of a GraphQL error
type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// AutoMergeInput represents the input for enabling auto-merge
type AutoMergeInput struct {
	PullRequestID string `json:"pullRequestId"`
	MergeMethod   string `json:"mergeMethod,omitempty"` // MERGE, SQUASH, or REBASE
}

// AutoMergeResponse represents the response from enabling auto-merge
type AutoMergeResponse struct {
	EnablePullRequestAutoMerge struct {
		PullRequest struct {
			ID               string `json:"id"`
			AutoMergeRequest *struct {
				EnabledAt string `json:"enabledAt"`
				EnabledBy struct {
					Login string `json:"login"`
				} `json:"enabledBy"`
				MergeMethod string `json:"mergeMethod"`
			} `json:"autoMergeRequest"`
		} `json:"pullRequest"`
	} `json:"enablePullRequestAutoMerge"`
}

// EnableAutoMerge enables auto-merge for a pull request using GraphQL
func (c *GraphQLClient) EnableAutoMerge(ctx context.Context, pullRequestID string, mergeMethod string) (*AutoMergeResponse, error) {
	slog.Debug("Enabling auto-merge via GraphQL", "pr_id", pullRequestID, "merge_method", mergeMethod)

	// Default to SQUASH if no method specified
	if mergeMethod == "" {
		mergeMethod = "SQUASH"
	}

	mutation := `
		mutation EnableAutoMerge($input: EnablePullRequestAutoMergeInput!) {
			enablePullRequestAutoMerge(input: $input) {
				pullRequest {
					id
					autoMergeRequest {
						enabledAt
						enabledBy {
							login
						}
						mergeMethod
					}
				}
			}
		}
	`

	variables := map[string]interface{}{
		"input": AutoMergeInput{
			PullRequestID: pullRequestID,
			MergeMethod:   mergeMethod,
		},
	}

	response, err := c.executeQuery(ctx, mutation, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to execute auto-merge mutation: %w", err)
	}

	var result AutoMergeResponse
	if err := json.Unmarshal(response.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse auto-merge response: %w", err)
	}

	slog.Info("Auto-merge enabled successfully", "pr_id", pullRequestID, "merge_method", mergeMethod)
	return &result, nil
}

// GetPullRequestNodeID gets the GraphQL node ID for a pull request
// This is needed because GraphQL uses different IDs than the REST API
func (c *GraphQLClient) GetPullRequestNodeID(ctx context.Context, owner, repo string, number int) (string, error) {
	slog.Debug("Getting PR node ID via GraphQL", "owner", owner, "repo", repo, "number", number)

	query := `
		query GetPullRequestNodeID($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				pullRequest(number: $number) {
					id
				}
			}
		}
	`

	variables := map[string]interface{}{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}

	response, err := c.executeQuery(ctx, query, variables)
	if err != nil {
		return "", fmt.Errorf("failed to get PR node ID: %w", err)
	}

	var result struct {
		Repository struct {
			PullRequest struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(response.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse node ID response: %w", err)
	}

	if result.Repository.PullRequest.ID == "" {
		return "", fmt.Errorf("pull request not found or no ID returned")
	}

	slog.Debug("Retrieved PR node ID", "node_id", result.Repository.PullRequest.ID)
	return result.Repository.PullRequest.ID, nil
}

// formatGraphQLError converts common GraphQL error messages to user-friendly messages
func formatGraphQLError(message string) string {
	lowerMsg := strings.ToLower(message)

	// Common auto-merge error scenarios
	if strings.Contains(lowerMsg, "pull request is in clean status") {
		return "Cannot enable auto-merge: pull request has no failing checks to resolve. Auto-merge is only available when there are pending or failing checks that need to pass first."
	}

	if strings.Contains(lowerMsg, "pull request is not mergeable") {
		return "Cannot enable auto-merge: pull request is not in a mergeable state. This could be due to merge conflicts, required status checks failing, or branch protection rules."
	}

	if strings.Contains(lowerMsg, "auto-merge is already enabled") {
		return "Auto-merge is already enabled for this pull request."
	}

	if strings.Contains(lowerMsg, "pull request is closed") {
		return "Cannot enable auto-merge: pull request is closed."
	}

	if strings.Contains(lowerMsg, "pull request is merged") {
		return "Cannot enable auto-merge: pull request is already merged."
	}

	if strings.Contains(lowerMsg, "pull request is draft") {
		return "Cannot enable auto-merge: pull request is in draft status. Please mark it as ready for review first."
	}

	if strings.Contains(lowerMsg, "insufficient permissions") || strings.Contains(lowerMsg, "permission") {
		return "Cannot enable auto-merge: insufficient permissions. You may need write access to the repository or admin permissions depending on branch protection settings."
	}

	if strings.Contains(lowerMsg, "branch protection") {
		return "Cannot enable auto-merge: branch protection rules prevent auto-merge. Check the repository's branch protection settings."
	}

	// Return empty string if no friendly message found
	return ""
}

// executeQuery executes a GraphQL query/mutation
func (c *GraphQLClient) executeQuery(ctx context.Context, query string, variables map[string]interface{}) (*GraphQLResponse, error) {
	payload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	slog.Debug("Executing GraphQL request", "url", req.URL.String())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	slog.Debug("GraphQL response received", "status", resp.StatusCode, "body_size", len(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GraphQL request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var graphqlResp GraphQLResponse
	if err := json.Unmarshal(body, &graphqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	if len(graphqlResp.Errors) > 0 {
		// Provide more user-friendly error messages for common auto-merge failures
		for _, err := range graphqlResp.Errors {
			if friendlyMsg := formatGraphQLError(err.Message); friendlyMsg != "" {
				return nil, fmt.Errorf("%s", friendlyMsg)
			}
		}
		// Fallback to generic error if no friendly message found
		return nil, fmt.Errorf("GraphQL errors: %+v", graphqlResp.Errors)
	}

	return &graphqlResp, nil
}
