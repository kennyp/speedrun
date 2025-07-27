package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cenkalti/backoff/v4"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
)

// Recommendation represents the AI's recommendation for a PR
type Recommendation string

const (
	Approve    Recommendation = "APPROVE"
	Review     Recommendation = "REVIEW"
	DeepReview Recommendation = "DEEP_REVIEW"
)

// Analysis represents the AI's analysis of a PR
type Analysis struct {
	Recommendation Recommendation
	Reasoning      string
	RiskLevel      string
}

// Agent wraps the OpenAI client for PR analysis
type Agent struct {
	client        *openai.Client
	model         string
	backoffConfig backoffconfig.Config
}

// NewAgent creates a new AI agent
func NewAgent(baseURL, apiKey, model string, backoffConfig backoffconfig.Config) *Agent {
	var opts []option.RequestOption

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(append(opts, option.WithAPIKey(apiKey))...)

	return &Agent{
		client:        &client,
		model:         model,
		backoffConfig: backoffConfig,
	}
}

// AnalyzePR analyzes a PR and returns a recommendation
func (a *Agent) AnalyzePR(ctx context.Context, prData PRData) (*Analysis, error) {
	prompt := a.buildPrompt(prData)

	var response *openai.ChatCompletion
	operation := func() error {
		var apiErr error
		response, apiErr = a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage(prompt),
			},
			Model: a.model,
		})
		return apiErr
	}

	exponentialBackoff := a.backoffConfig.ToExponentialBackoff()
	err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get AI response: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response from AI model")
	}

	content := response.Choices[0].Message.Content
	return a.parseResponse(content), nil
}

// PRData represents the data about a PR for analysis
type PRData struct {
	Title        string
	Number       int
	Additions    int
	Deletions    int
	ChangedFiles int
	CIStatus     string
	Reviews      []ReviewInfo
}

// ReviewInfo represents information about a review
type ReviewInfo struct {
	State string
	User  string
}

func (a *Agent) buildPrompt(pr PRData) string {
	return fmt.Sprintf(`Analyze this GitHub pull request and provide a recommendation for an on-call engineer:

PR: #%d - %s

**Changes:**
- Files changed: %d
- Lines added: %d  
- Lines deleted: %d
- Total changes: %d

**CI Status:** %s

**Existing Reviews:** %s

Based on this information, recommend one of:
- APPROVE: Safe to quickly approve (simple changes, passing CI, low risk)
- REVIEW: Needs careful review (moderate complexity or unclear status)  
- DEEP_REVIEW: Requires thorough investigation (complex changes, failing CI, high risk)

Respond in this format:
RECOMMENDATION: [APPROVE/REVIEW/DEEP_REVIEW]
RISK_LEVEL: [LOW/MEDIUM/HIGH]
REASONING: [Brief explanation of why you made this recommendation]`,
		pr.Number, pr.Title, pr.ChangedFiles, pr.Additions, pr.Deletions,
		pr.Additions+pr.Deletions, pr.CIStatus, a.formatReviews(pr.Reviews))
}

func (a *Agent) formatReviews(reviews []ReviewInfo) string {
	if len(reviews) == 0 {
		return "None"
	}

	var reviewStrs []string
	for _, review := range reviews {
		reviewStrs = append(reviewStrs, fmt.Sprintf("%s: %s", review.User, review.State))
	}
	return strings.Join(reviewStrs, ", ")
}

func (a *Agent) parseResponse(content string) *Analysis {
	lines := strings.Split(content, "\n")

	analysis := &Analysis{
		Recommendation: Review, // default
		RiskLevel:      "MEDIUM",
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "RECOMMENDATION:") {
			rec := strings.TrimSpace(strings.TrimPrefix(line, "RECOMMENDATION:"))
			switch rec {
			case "APPROVE":
				analysis.Recommendation = Approve
			case "REVIEW":
				analysis.Recommendation = Review
			case "DEEP_REVIEW":
				analysis.Recommendation = DeepReview
			}
		} else if strings.HasPrefix(line, "RISK_LEVEL:") {
			analysis.RiskLevel = strings.TrimSpace(strings.TrimPrefix(line, "RISK_LEVEL:"))
		} else if strings.HasPrefix(line, "REASONING:") {
			analysis.Reasoning = strings.TrimSpace(strings.TrimPrefix(line, "REASONING:"))
		}
	}

	return analysis
}

