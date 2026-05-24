// Package endpoint provides LLM endpoint management with multi-provider support.
package endpoint

import (
	"context"
	"encoding/json"
)

// ToolDefinition is sent to the LLM to advertise a callable tool.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"` // JSON Schema for the tool's parameters
}

// ToolCall is one invocation request emitted by the assistant.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ChatMessage is the unified message format for all LLM providers.
// It represents a single message in a chat conversation.
type ChatMessage struct {
	// Role identifies the message sender: "system", "user", "assistant", or "tool"
	Role string `json:"role"`

	// Content contains the message text
	Content string `json:"content"`

	// ToolCalls is set on assistant turns that called tools
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolCallID is set on role="tool" responses to identify which call this answers
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Name is the tool name on role="tool" responses
	Name string `json:"name,omitempty"`
}

// ChatOptions provides generation parameters for chat completion requests.
type ChatOptions struct {
	// MaxTokens limits the maximum number of tokens to generate
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature controls randomness (0.0 = deterministic, 2.0 = very random)
	Temperature float64 `json:"temperature,omitempty"`

	// Stream enables streaming response mode
	Stream bool `json:"stream,omitempty"`

	// Stop sequences that will halt generation when encountered
	Stop []string `json:"stop,omitempty"`

	// EnableThinking enables extended thinking mode (Anthropic/Z.ai)
	EnableThinking bool `json:"enable_thinking,omitempty"`

	// ThinkingBudget sets the token budget for thinking (Anthropic)
	ThinkingBudget int `json:"thinking_budget,omitempty"`

	// Tools advertises callable tools to the LLM
	Tools []ToolDefinition `json:"tools,omitempty"`
}

// TokenUsage contains token consumption metrics for a request.
type TokenUsage struct {
	// PromptTokens is the number of tokens in the input prompt
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the number of tokens in the generated response
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is the sum of prompt and completion tokens
	TotalTokens int `json:"total_tokens"`
}

// ResponseMetrics contains optional provider-specific performance metrics.
type ResponseMetrics struct {
	// GenerationThroughput is tokens per second for generation (vLLM)
	GenerationThroughput float64 `json:"generation_throughput,omitempty"`

	// PromptThroughput is tokens per second for prompt processing (vLLM)
	PromptThroughput float64 `json:"prompt_throughput,omitempty"`

	// KVCacheUsagePercent is the KV cache utilization percentage (vLLM)
	KVCacheUsagePercent float64 `json:"kv_cache_usage_percent,omitempty"`

	// PrefixCacheHitRatePercent is the prefix cache hit rate (vLLM)
	PrefixCacheHitRatePercent float64 `json:"prefix_cache_hit_rate_percent,omitempty"`

	// RequestsRunning is the number of currently running requests (vLLM)
	RequestsRunning int `json:"requests_running,omitempty"`

	// RequestsWaiting is the number of queued requests (vLLM)
	RequestsWaiting int `json:"requests_waiting,omitempty"`

	// ProviderSpecific holds additional provider-specific metrics
	ProviderSpecific map[string]any `json:"provider_specific,omitempty"`
}

// ChatResponse is the unified response format for chat completions.
type ChatResponse struct {
	// Content contains the generated response text
	Content string `json:"content"`

	// Model identifies the model that generated the response
	Model string `json:"model"`

	// TokensUsed contains token consumption metrics
	TokensUsed TokenUsage `json:"tokens_used"`

	// FinishReason indicates why generation stopped (e.g., "stop", "length")
	FinishReason string `json:"finish_reason"`

	// Thinking contains optional thinking/reasoning content (if enabled)
	Thinking string `json:"thinking,omitempty"`

	// Metrics contains optional provider-specific performance metrics
	Metrics *ResponseMetrics `json:"metrics,omitempty"`

	// ToolCalls is populated when the assistant called tools
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChatClient is the unified chat interface that abstracts provider differences.
// This interface is implemented by provider-specific adapters.
type ChatClient interface {
	// Chat sends a chat completion request and returns the response.
	Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error)

	// ChatStream sends a chat completion request and streams the response.
	// The callback is invoked for each chunk of the response text.
	// Returns any tool-calls the assistant made (finalized after stream ends).
	// Adapters that do not yet support tool-calls return nil, nil.
	ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error)

	// Embed generates embeddings for the given texts.
	Embed(ctx context.Context, model string, texts []string) ([][]float64, error)
}

// ChatClientAdapter is the interface for provider-specific implementations.
// Provider adapters implement this interface to handle their native APIs.
type ChatClientAdapter interface {
	// Chat sends a chat completion request using the provider's native API.
	Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error)

	// ChatStream sends a streaming chat request using the provider's native API.
	// Returns any tool-calls the assistant made (finalized after stream ends).
	// Adapters that do not yet support tool-calls return nil, nil.
	ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error)

	// Embed generates embeddings using the provider's embedding API.
	Embed(ctx context.Context, model string, texts []string) ([][]float64, error)

	// ProviderName returns the name of the provider (e.g., "ollama", "openai", "anthropic")
	ProviderName() string
}
