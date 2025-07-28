package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	_ "embed"

	"github.com/cenkalti/backoff/v4"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed prompts/developer.md
var DeveloperMessage string

//go:embed prompts/review.tmpl.md
var ReviewMessageTemplate string

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
	prompt, err := a.buildPrompt(prData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate prompt (%w)", err)
	}

	var response *openai.ChatCompletion
	operation := func() error {
		var apiErr error
		response, apiErr = a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.DeveloperMessage(DeveloperMessage),
				openai.UserMessage(prompt),
			},
			Model: a.model,
		})
		return apiErr
	}

	exponentialBackoff := a.backoffConfig.ToExponentialBackoff()
	if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
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

func (a *Agent) buildPrompt(pr PRData) (string, error) {
	funcMap := template.FuncMap{
		"sum": func(a, b int) int {
			return a + b
		},
	}
	t := template.Must(template.New("review").Funcs(funcMap).Parse(ReviewMessageTemplate))

	var prompt bytes.Buffer
	if err := t.Execute(&prompt, pr); err != nil {
		return "", err
	}

	return prompt.String(), nil
}

func (a *Agent) parseResponse(content string) *Analysis {
	lines := strings.Split(content, "\n")

	analysis := &Analysis{
		Recommendation: Review, // default
		RiskLevel:      "MEDIUM",
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "RECOMMENDATION:"); ok {
			rec := strings.TrimSpace(after)
			switch rec {
			case "APPROVE":
				analysis.Recommendation = Approve
			case "REVIEW":
				analysis.Recommendation = Review
			case "DEEP_REVIEW":
				analysis.Recommendation = DeepReview
			}
		} else if after, ok := strings.CutPrefix(line, "RISK_LEVEL:"); ok {
			analysis.RiskLevel = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "REASONING:"); ok {
			analysis.Reasoning = strings.TrimSpace(after)
		}
	}

	return analysis
}
