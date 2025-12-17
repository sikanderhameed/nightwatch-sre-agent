// Package llm provides LLM integration for the Nightwatch Operator.
// This is the "AI Brain" that transforms Nightwatch from a traditional
// operator into an AI-First self-healing system.
//
// Updated to use Groq API (OpenAI-compatible) for fast, free inference.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// Diagnosis represents the AI's analysis of a Kubernetes issue.
type Diagnosis struct {
	RootCause   string  `json:"root_cause"`
	Severity    string  `json:"severity"` // low, medium, high, critical
	CanAutoFix  bool    `json:"can_auto_fix"`
	FixCommand  string  `json:"fix_command"`
	Confidence  float64 `json:"confidence"` // 0.0 to 1.0
	Explanation string  `json:"explanation"`
}

// Client wraps the OpenAI-compatible client for Groq.
type Client struct {
	client *openai.Client
	model  string
}

// NewClient creates a new LLM client using the GROQ_API_KEY env var.
func NewClient() *Client {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		// Fallback to GOOGLE_API_KEY for backward compatibility
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	
	if apiKey == "" {
		fmt.Println("⚠️ No API key found (GROQ_API_KEY or GOOGLE_API_KEY)")
		return nil
	}
	
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "llama-3.1-70b-versatile" // Groq's best free model
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://api.groq.com/openai/v1"
	client := openai.NewClientWithConfig(config)

	fmt.Printf("✅ Groq LLM client initialized with model: %s\n", model)

	return &Client{
		client: client,
		model:  model,
	}
}

// DiagnoseScalingFailure asks the LLM to diagnose why a scaling operation failed.
func (c *Client) DiagnoseScalingFailure(ctx context.Context, opts DiagnoseOptions) (*Diagnosis, error) {
	if c.client == nil {
		return nil, fmt.Errorf("LLM client is not initialized")
	}

	prompt := buildDiagnosisPrompt(opts)
	
	// Retry logic with exponential backoff
	maxRetries := 3
	var lastErr error
	
	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			// Exponential backoff: 2s, 4s, 8s
			backoff := time.Duration(1<<uint(i)) * 2 * time.Second
			fmt.Printf("⚠️ LLM Rate limited (429). Retrying in %v (attempt %d/%d)...\n", backoff, i, maxRetries)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		
		resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are an expert Kubernetes SRE AI. Respond ONLY with valid JSON.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: 0.3, // Lower temperature for more consistent JSON
			MaxTokens:   1000,
		})
		
		if err != nil {
			// Check if it's a rate limit error (429)
			if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "quota") || strings.Contains(err.Error(), "rate") {
				lastErr = err
				continue // Retry
			}
			return nil, fmt.Errorf("LLM API error: %w", err)
		}

		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no response from LLM")
		}

		responseText := resp.Choices[0].Message.Content

		// Parse JSON response
		var diagnosis Diagnosis
		if err := json.Unmarshal([]byte(responseText), &diagnosis); err != nil {
			return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw: %s", err, responseText)
		}

		return &diagnosis, nil
	}
	
	return nil, fmt.Errorf("max retries exceeded after rate limit: %w", lastErr)
}

// DiagnoseOptions contains all the context needed for diagnosis.
type DiagnoseOptions struct {
	DeploymentName   string
	Namespace        string
	Events           string
	PodStatus        string
	ReplicaSetInfo   string
	Logs             string
	KnowledgeContext string // RAG: Retrieved runbook knowledge
}

func buildDiagnosisPrompt(opts DiagnoseOptions) string {
	// Build base prompt
	basePrompt := fmt.Sprintf(`You are an expert Kubernetes SRE AI. A deployment is failing to scale.

Deployment: %s
Namespace: %s

Recent Events:
%s

Pod Status:
%s

ReplicaSet Info:
%s

Recent Logs:
%s`,
		opts.DeploymentName,
		opts.Namespace,
		opts.Events,
		opts.PodStatus,
		opts.ReplicaSetInfo,
		opts.Logs,
	)
	
	// Add RAG knowledge context if available
	if opts.KnowledgeContext != "" {
		basePrompt += fmt.Sprintf(`

---
%s
---

Use the above internal runbook knowledge to inform your diagnosis. This knowledge comes from past incidents and company-specific solutions.`,
			opts.KnowledgeContext,
		)
	}
	
	// Add instructions
	basePrompt += `

Analyze this and provide a JSON response with:
{
  "root_cause": "brief technical explanation",
  "severity": "low|medium|high|critical",
  "can_auto_fix": true|false,
  "fix_command": "kubectl command to fix (if can_auto_fix is true)",
  "confidence": 0.0-1.0,
  "explanation": "detailed explanation for the team"
}

Focus on:
1. What is the actual failure mode?
2. Can this be fixed automatically with kubectl?
3. What's the safest remediation?

Respond ONLY with valid JSON.`
	
	return basePrompt
}

// Close closes the LLM client (no-op for OpenAI client).
func (c *Client) Close() error {
	return nil
}
