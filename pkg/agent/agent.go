package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/cenkalti/backoff/v4"
	backoffconfig "github.com/kennyp/speedrun/pkg/backoff"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
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
	toolRegistry  *ToolRegistry
	toolTimeout   time.Duration
}

// NewAgent creates a new AI agent
func NewAgent(baseURL, apiKey, model string, backoffConfig backoffconfig.Config, toolRegistry *ToolRegistry, toolTimeout time.Duration) *Agent {
	var opts []option.RequestOption

	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	client := openai.NewClient(append(opts, option.WithAPIKey(apiKey))...)

	return &Agent{
		client:        &client,
		model:         model,
		backoffConfig: backoffConfig,
		toolRegistry:  toolRegistry,
		toolTimeout:   toolTimeout,
	}
}

// AnalyzePR analyzes a PR and returns a recommendation
func (a *Agent) AnalyzePR(ctx context.Context, prData PRData) (*Analysis, error) {
	prompt, err := a.buildPrompt(prData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate prompt (%w)", err)
	}

	// Initialize the conversation
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.DeveloperMessage(DeveloperMessage),
		openai.UserMessage(prompt),
	}

	// Execute conversation with tool support
	finalResponse, err := a.executeConversation(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to execute conversation: %w", err)
	}

	return a.parseResponse(finalResponse), nil
}

// executeConversation handles the conversation loop with tool calling support
func (a *Agent) executeConversation(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion) (string, error) {
	const maxIterations = 10 // Prevent infinite loops
	
	for iteration := 0; iteration < maxIterations; iteration++ {
		slog.Debug("Executing conversation iteration", slog.Int("iteration", iteration))
		
		// Prepare chat completion parameters
		params := openai.ChatCompletionNewParams{
			Messages: messages,
			Model:    a.model,
		}
		
		// Add tools if available
		if a.toolRegistry != nil {
			tools := a.toolRegistry.GetOpenAITools()
			if len(tools) > 0 {
				params.Tools = tools
			}
		}

		var response *openai.ChatCompletion
		operation := func() error {
			var apiErr error
			response, apiErr = a.client.Chat.Completions.New(ctx, params)
			return apiErr
		}

		exponentialBackoff := a.backoffConfig.ToExponentialBackoff()
		if err := backoff.Retry(operation, backoff.WithContext(exponentialBackoff, ctx)); err != nil {
			return "", fmt.Errorf("failed to get AI response: %w", err)
		}

		if len(response.Choices) == 0 {
			return "", fmt.Errorf("no response from AI model")
		}

		choice := response.Choices[0]
		
		// Check if the assistant wants to use tools
		if choice.Message.ToolCalls != nil && len(choice.Message.ToolCalls) > 0 {
			slog.Debug("Processing tool calls", slog.Int("count", len(choice.Message.ToolCalls)))
			
			// Create assistant message with tool calls
			var assistant openai.ChatCompletionAssistantMessageParam
			if choice.Message.Content != "" {
				assistant.Content.OfString = param.NewOpt(choice.Message.Content)
			}
			
			// Convert tool calls to the parameter format
			assistant.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, len(choice.Message.ToolCalls))
			for i, toolCall := range choice.Message.ToolCalls {
				assistant.ToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
					ID:   toolCall.ID,
					Type: "function",
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
			
			messages = append(messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
			
			// Execute tool calls
			for _, toolCall := range choice.Message.ToolCalls {
				result, err := a.executeToolCall(ctx, toolCall)
				if err != nil {
					slog.Error("Tool call failed", slog.String("tool", toolCall.Function.Name), slog.Any("error", err))
					result = fmt.Sprintf("Error: %v", err)
				}
				
				// Add tool result to conversation
				messages = append(messages, openai.ToolMessage(result, toolCall.ID))
			}
			
			// Continue the conversation to get the final response
			continue
		}
		
		// No tool calls, add regular assistant message and return
		messages = append(messages, openai.AssistantMessage(choice.Message.Content))
		return choice.Message.Content, nil
	}
	
	return "", fmt.Errorf("conversation exceeded maximum iterations (%d)", maxIterations)
}

// executeToolCall executes a single tool call
func (a *Agent) executeToolCall(ctx context.Context, toolCall openai.ChatCompletionMessageToolCall) (string, error) {
	if a.toolRegistry == nil {
		return "", fmt.Errorf("no tool registry available")
	}
	
	tool, exists := a.toolRegistry.Get(toolCall.Function.Name)
	if !exists {
		return "", fmt.Errorf("unknown tool: %s", toolCall.Function.Name)
	}
	
	slog.Debug("Executing tool", slog.String("name", toolCall.Function.Name), slog.String("args", toolCall.Function.Arguments))
	
	// Parse arguments as JSON
	var args json.RawMessage
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("invalid tool arguments: %w", err)
	}
	
	// Create a new context with a configurable timeout for tool execution
	// This should be longer than the GitHub backoff MaxElapsedTime (60s) to allow retries
	toolCtx, cancel := context.WithTimeout(context.Background(), a.toolTimeout)
	defer cancel()
	
	// Execute the tool with the dedicated context
	return tool.Execute(toolCtx, args)
}

// PRData represents the data about a PR for analysis
type PRData struct {
	Title         string
	Number        int
	Additions     int
	Deletions     int
	ChangedFiles  int
	CIStatus      string // Deprecated: Use CheckDetails instead
	CheckDetails  []CheckInfo
	Reviews       []ReviewInfo
	HasConflicts  bool
	PRURL         string
}

// CheckInfo represents information about a CI check
type CheckInfo struct {
	Name        string
	Status      string // success, failure, pending, error
	Description string
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
