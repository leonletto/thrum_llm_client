// Package endpoint provides LLM endpoint management with multi-provider support.
package endpoint

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ollamaClient implements ChatClientAdapter for Ollama's native API.
type ollamaClient struct {
	baseURL      string
	httpClient   *http.Client
	chatPath     string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func (c *ollamaClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *ollamaClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *ollamaClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }

// NewOllamaClient creates a new Ollama chat client adapter.
func NewOllamaClient(baseURL, chatPath string) *ollamaClient {
	return &ollamaClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{Timeout: 120 * time.Second},
		chatPath:   chatPath,
	}
}

// ProviderName returns the provider identifier.
func (c *ollamaClient) ProviderName() string {
	return "ollama"
}

// ollamaChatRequest is the request format for Ollama's /api/chat endpoint.
type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  *ollamaChatOptions  `json:"options,omitempty"`
}

// ollamaChatMessage is Ollama's message format.
type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaChatOptions contains generation parameters for Ollama.
type ollamaChatOptions struct {
	NumPredict  int      `json:"num_predict,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ollamaChatResponse is Ollama's response format.
type ollamaChatResponse struct {
	Model              string            `json:"model"`
	CreatedAt          string            `json:"created_at"`
	Message            ollamaChatMessage `json:"message"`
	Done               bool              `json:"done"`
	DoneReason         string            `json:"done_reason,omitempty"`
	TotalDuration      int64             `json:"total_duration"`
	LoadDuration       int64             `json:"load_duration"`
	PromptEvalCount    int               `json:"prompt_eval_count"`
	PromptEvalDuration int64             `json:"prompt_eval_duration"`
	EvalCount          int               `json:"eval_count"`
	EvalDuration       int64             `json:"eval_duration"`
}

// Chat sends a non-streaming chat completion request to Ollama.
func (c *ollamaClient) Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error) {
	// Convert messages to Ollama format
	ollamaMessages := make([]ollamaChatMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaChatMessage{Role: m.Role, Content: m.Content}
	}

	// Build request
	req := ollamaChatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   false,
	}

	// Apply options
	if opts != nil {
		req.Options = &ollamaChatOptions{}
		if opts.MaxTokens > 0 {
			req.Options.NumPredict = opts.MaxTokens
		}
		if opts.Temperature > 0 {
			req.Options.Temperature = opts.Temperature
		}
		if len(opts.Stop) > 0 {
			req.Options.Stop = opts.Stop
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}

	chatPath := "/api/chat"
	if c.chatPath != "" {
		chatPath = c.chatPath
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "Chat", respBody)
	}

	var ollamaResp ollamaChatResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama: failed to unmarshal response: %w", err)
	}

	// Convert to unified response format
	return c.convertResponse(&ollamaResp), nil
}

// ChatStream sends a streaming chat request to Ollama.
// Ollama uses newline-delimited JSON for streaming (not SSE).
// Tool-call support not yet implemented; returns nil tool-calls.
func (c *ollamaClient) ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error) {
	// Convert messages to Ollama format
	ollamaMessages := make([]ollamaChatMessage, len(messages))
	for i, m := range messages {
		ollamaMessages[i] = ollamaChatMessage{Role: m.Role, Content: m.Content}
	}

	// Build request with streaming enabled
	req := ollamaChatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Stream:   true,
	}

	// Apply options
	if opts != nil {
		req.Options = &ollamaChatOptions{}
		if opts.MaxTokens > 0 {
			req.Options.NumPredict = opts.MaxTokens
		}
		if opts.Temperature > 0 {
			req.Options.Temperature = opts.Temperature
		}
		if len(opts.Stop) > 0 {
			req.Options.Stop = opts.Stop
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}

	ollamaChatPath := "/api/chat"
	if c.chatPath != "" {
		ollamaChatPath = c.chatPath
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+ollamaChatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, c.handleError(resp, "ChatStream", respBody)
	}

	// Read newline-delimited JSON stream
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), streamScannerBufferMax)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return nil, fmt.Errorf("ollama: failed to unmarshal stream chunk: %w", err)
		}

		// Send content to callback
		if chunk.Message.Content != "" {
			if err := callback(chunk.Message.Content); err != nil {
				return nil, err
			}
		}

		// Stop when done
		if chunk.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ollama: stream read error: %w", err)
	}

	return nil, nil
}

// ollamaEmbedRequest is the request format for Ollama's /api/embeddings endpoint.
type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse is the response format for Ollama embeddings.
type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed generates embeddings for the given texts using Ollama's /api/embeddings endpoint.
func (c *ollamaClient) Embed(ctx context.Context, model string, texts []string) ([][]float64, error) {
	embeddings := make([][]float64, 0, len(texts))

	for _, text := range texts {
		req := ollamaEmbedRequest{
			Model:  model,
			Prompt: text,
		}

		body, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("ollama: failed to marshal embed request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("ollama: failed to create embed request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		applyExtraHeaders(httpReq, c.extraHeaders)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("ollama: embed request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("ollama: failed to read embed response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, c.handleError(resp, "Embed", respBody)
		}

		var embedResp ollamaEmbedResponse
		if err := json.Unmarshal(respBody, &embedResp); err != nil {
			return nil, fmt.Errorf("ollama: failed to unmarshal embed response: %w", err)
		}

		embeddings = append(embeddings, embedResp.Embedding)
	}

	return embeddings, nil
}

// convertResponse converts Ollama's response to the unified ChatResponse format.
func (c *ollamaClient) convertResponse(resp *ollamaChatResponse) *ChatResponse {
	// Calculate tokens per second for metrics
	var tokensPerSec float64
	if resp.EvalDuration > 0 {
		tokensPerSec = float64(resp.EvalCount) / (float64(resp.EvalDuration) / 1e9)
	}

	finishReason := resp.DoneReason
	if finishReason == "" && resp.Done {
		finishReason = "stop"
	}

	return &ChatResponse{
		Content:      resp.Message.Content,
		Model:        resp.Model,
		FinishReason: finishReason,
		TokensUsed: TokenUsage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
		Metrics: &ResponseMetrics{
			GenerationThroughput: tokensPerSec,
			ProviderSpecific: map[string]any{
				"total_duration":       resp.TotalDuration,
				"load_duration":        resp.LoadDuration,
				"prompt_eval_duration": resp.PromptEvalDuration,
				"eval_duration":        resp.EvalDuration,
			},
		},
	}
}

// Compile-time interface verification
var _ ChatClientAdapter = (*ollamaClient)(nil)

// handleError parses an Ollama error body (flat {"error":"..."}) and
// returns an EndpointError wrapping the typed sentinel for the status
// code. Mirrors the anthropic/openai/zai pattern.
func (c *ollamaClient) handleError(resp *http.Response, op string, body []byte) error {
	var apiMsg string
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		apiMsg = errResp.Error
	}
	sent := httpStatusToSentinel(resp.StatusCode)
	if sent == nil {
		if apiMsg != "" {
			return NewEndpointError(c.baseURL, "ollama", op, fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, "ollama", op, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, "ollama", op, fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, "ollama", op, sent)
}
