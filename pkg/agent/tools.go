package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kennyp/speedrun/pkg/cache"
	"github.com/kennyp/speedrun/pkg/github"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
)

// Tool represents a tool that the agent can use
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (string, error)
}

// ToolRegistry holds all available tools
type ToolRegistry struct {
	tools  map[string]Tool
	client *github.Client
	cache  cache.Cache
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry(githubClient *github.Client, cache cache.Cache) *ToolRegistry {
	registry := &ToolRegistry{
		tools:  make(map[string]Tool),
		client: githubClient,
		cache:  cache,
	}

	// Register all tools
	registry.Register(&GitHubTool{client: githubClient, cache: cache})
	registry.Register(&WebFetchTool{cache: cache})
	registry.Register(&DiffAnalyzerTool{cache: cache})

	return registry
}

// Register adds a tool to the registry
func (r *ToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// GetOpenAITools returns tool definitions for OpenAI API
func (r *ToolRegistry) GetOpenAITools() []openai.ChatCompletionToolParam {
	var tools []openai.ChatCompletionToolParam

	for _, tool := range r.tools {
		var params openai.FunctionParameters
		if err := json.Unmarshal(tool.Parameters(), &params); err != nil {
			// Log error but continue with empty params
			slog.Error("Failed to unmarshal tool parameters", slog.String("tool", tool.Name()), slog.Any("error", err))
			params = openai.FunctionParameters{}
		}

		tools = append(tools, openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        tool.Name(),
				Description: param.NewOpt(tool.Description()),
				Parameters:  params,
			},
		})
	}

	return tools
}

// GitHubTool provides GitHub API access
type GitHubTool struct {
	client *github.Client
	cache  cache.Cache
}

func (t *GitHubTool) Name() string {
	return "github_api"
}

func (t *GitHubTool) Description() string {
	return "Access GitHub API to get PR details, diffs, file contents, and comments. Essential for dependency updates: check PR comments for links to release notes, changelogs, and security advisories. Use get_pr_comments to find upstream information that explains what changed between versions."
}

func (t *GitHubTool) Parameters() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"get_pr_details", "get_pr_diff", "get_file_content", "get_pr_comments"},
				"description": "The action to perform: get_pr_details for basic info, get_pr_diff for code changes, get_file_content for specific files, get_pr_comments for links to release notes/changelogs",
			},
			"owner": map[string]interface{}{
				"type":        "string",
				"description": "Repository owner",
			},
			"repo": map[string]interface{}{
				"type":        "string",
				"description": "Repository name",
			},
			"pr_number": map[string]interface{}{
				"type":        "integer",
				"description": "Pull request number",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File path (for get_file_content)",
			},
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "Git ref (for get_file_content)",
			},
		},
		"required": []string{"action", "owner", "repo"},
	}

	data, _ := json.Marshal(schema)
	return data
}

type githubToolParams struct {
	Action   string `json:"action"`
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number,omitempty"`
	Path     string `json:"path,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

func (t *GitHubTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p githubToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// Generate cache key based on tool name and parameters
	cacheKey := t.generateCacheKey(params)

	// Try to get from cache first
	var result string
	if err := t.cache.Get(cacheKey, &result); err == nil {
		return result, nil
	}

	// Cache miss, execute the operation
	switch p.Action {
	case "get_pr_details":
		result, err := t.client.GetPRDetails(ctx, p.Owner, p.Repo, p.PRNumber)
		if err != nil {
			return "", err
		}
		if err := t.cache.Set(cacheKey, result); err != nil {
			slog.Error("Failed to cache GitHub API result", slog.String("key", cacheKey), slog.Any("error", err))
		}
		return result, nil

	case "get_pr_diff":
		result, err := t.client.GetPRDiff(ctx, p.Owner, p.Repo, p.PRNumber)
		if err != nil {
			return "", err
		}
		if err := t.cache.Set(cacheKey, result); err != nil {
			slog.Error("Failed to cache GitHub API result", slog.String("key", cacheKey), slog.Any("error", err))
		}
		return result, nil

	case "get_file_content":
		if p.Path == "" {
			return "", fmt.Errorf("path parameter is required for get_file_content")
		}
		result, err := t.client.GetFileContent(ctx, p.Owner, p.Repo, p.Path, p.Ref)
		if err != nil {
			return "", err
		}
		if err := t.cache.Set(cacheKey, result); err != nil {
			slog.Error("Failed to cache GitHub API result", slog.String("key", cacheKey), slog.Any("error", err))
		}
		return result, nil

	case "get_pr_comments":
		result, err := t.client.GetPRComments(ctx, p.Owner, p.Repo, p.PRNumber)
		if err != nil {
			return "", err
		}
		if err := t.cache.Set(cacheKey, result); err != nil {
			slog.Error("Failed to cache GitHub API result", slog.String("key", cacheKey), slog.Any("error", err))
		}
		return result, nil

	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

func (t *GitHubTool) generateCacheKey(params json.RawMessage) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("github_api:%s", string(params))))
	return fmt.Sprintf("tool:github:%x", hash)
}

// WebFetchTool fetches content from URLs
type WebFetchTool struct {
	cache cache.Cache
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return "Fetch content from URLs including release notes, changelogs, security advisories, and documentation. Critical for dependency analysis: fetch upstream project information to understand what actually changed, not just the diff size. Look for links in PR descriptions and comments."
}

func (t *WebFetchTool) Parameters() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch",
			},
		},
		"required": []string{"url"},
	}

	data, _ := json.Marshal(schema)
	return data
}

type webFetchParams struct {
	URL string `json:"url"`
}

func (t *WebFetchTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p webFetchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// Generate cache key based on tool name and parameters
	cacheKey := t.generateCacheKey(params)

	// Try to get from cache first
	var result string
	if err := t.cache.Get(cacheKey, &result); err == nil {
		return result, nil
	}

	// Cache miss, fetch from URL
	// Create a request with context
	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching URL: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Error("Failed to close response body", slog.Any("error", closeErr))
		}
	}()

	// Check for HTTP errors (don't cache error responses)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP error %d: %s", resp.StatusCode, resp.Status)
	}

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	// Return first 5000 characters to avoid overwhelming the model
	content := string(body)
	if len(content) > 5000 {
		content = content[:5000] + "\n... (truncated)"
	}

	// Cache the successful result
	if err := t.cache.Set(cacheKey, content); err != nil {
		slog.Error("Failed to cache web fetch result", slog.String("key", cacheKey), slog.Any("error", err))
	}

	return content, nil
}

func (t *WebFetchTool) generateCacheKey(params json.RawMessage) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("web_fetch:%s", string(params))))
	return fmt.Sprintf("tool:web:%x", hash)
}

// DiffAnalyzerTool analyzes diffs for sensitive changes
type DiffAnalyzerTool struct {
	cache cache.Cache
}

func (t *DiffAnalyzerTool) Name() string {
	return "diff_analyzer"
}

func (t *DiffAnalyzerTool) Description() string {
	return "Analyze diffs for sensitive file changes and modified paths. For dependency updates: use to distinguish between vendored dependency files (which should be ignored) and actual source code changes. Focus analysis on non-vendor paths to identify real code changes."
}

func (t *DiffAnalyzerTool) Parameters() json.RawMessage {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"diff": map[string]interface{}{
				"type":        "string",
				"description": "The diff content to analyze",
			},
			"analysis_type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"sensitive_files", "modified_paths"},
				"description": "Type of analysis: sensitive_files to detect security-related changes, modified_paths to list all changed files (useful for filtering out vendor/dependencies)",
			},
		},
		"required": []string{"diff", "analysis_type"},
	}

	data, _ := json.Marshal(schema)
	return data
}

type diffAnalyzerParams struct {
	Diff         string `json:"diff"`
	AnalysisType string `json:"analysis_type"`
}

func (t *DiffAnalyzerTool) Execute(ctx context.Context, params json.RawMessage) (string, error) {
	var p diffAnalyzerParams
	if err := json.Unmarshal(params, &p); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// Generate cache key based on tool name and parameters
	cacheKey := t.generateCacheKey(params)

	// Try to get from cache first
	var result string
	if err := t.cache.Get(cacheKey, &result); err == nil {
		return result, nil
	}

	// Cache miss, perform analysis
	var analysisResult string
	switch p.AnalysisType {
	case "sensitive_files":
		analysisResult = t.analyzeSensitiveFiles(p.Diff)

	case "modified_paths":
		analysisResult = t.getModifiedPaths(p.Diff)

	default:
		return "", fmt.Errorf("unknown analysis type: %s", p.AnalysisType)
	}

	// Cache the successful result
	if err := t.cache.Set(cacheKey, analysisResult); err != nil {
		slog.Error("Failed to cache diff analysis result", slog.String("key", cacheKey), slog.Any("error", err))
	}

	return analysisResult, nil
}

func (t *DiffAnalyzerTool) generateCacheKey(params json.RawMessage) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("diff_analyzer:%s", string(params))))
	return fmt.Sprintf("tool:diff:%x", hash)
}

func (t *DiffAnalyzerTool) analyzeSensitiveFiles(diff string) string {
	sensitivePatterns := []string{
		"auth", "password", "secret", "token", "key", "credential",
		"config", "env", ".env", "database", "db",
		"security", "permission", "access",
	}

	var findings []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			// Check file paths
			lowerLine := strings.ToLower(line)
			for _, pattern := range sensitivePatterns {
				if strings.Contains(lowerLine, pattern) {
					findings = append(findings, fmt.Sprintf("Sensitive file pattern '%s' found in: %s", pattern, line))
				}
			}
		}
	}

	if len(findings) == 0 {
		return "No sensitive file patterns detected in the diff."
	}

	return "Sensitive file analysis:\n" + strings.Join(findings, "\n")
}

func (t *DiffAnalyzerTool) getModifiedPaths(diff string) string {
	var paths []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		if strings.HasPrefix(line, "+++") {
			// Extract file path from +++ b/path/to/file
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				path := strings.TrimPrefix(parts[1], "b/")
				paths = append(paths, path)
			}
		}
	}

	if len(paths) == 0 {
		return "No file paths found in the diff."
	}

	return "Modified files:\n" + strings.Join(paths, "\n")
}
