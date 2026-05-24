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

// openaiCompatClient is an HTTP client for OpenAI-compatible APIs (OpenAI, vLLM, etc.).
// It implements the ChatClientAdapter interface for unified LLM access.
type openaiCompatClient struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	chatPath     string
	extraHeaders map[string]string
	extraBody    map[string]any
	provider     string
}

func (c *openaiCompatClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *openaiCompatClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *openaiCompatClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }

// NewOpenAICompatClient creates a new OpenAI-compatible chat client.
// providerName controls what ProviderName() returns for model registry lookups.
func NewOpenAICompatClient(baseURL, apiKey, chatPath string, extraHeaders map[string]string, providerName string) *openaiCompatClient {
	if providerName == "" {
		providerName = "openai-compat"
	}
	return &openaiCompatClient{
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		chatPath:     chatPath,
		extraHeaders: extraHeaders,
		provider:     providerName,
	}
}

// ProviderName returns the name of this provider for model registry lookups.
func (c *openaiCompatClient) ProviderName() string {
	return c.provider
}

// openaiChatRequest represents the OpenAI chat completion request format.
type openaiChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
	Stop        []string      `json:"stop,omitempty"`
}

// openaiChatChoice represents a choice in the OpenAI response.
type openaiChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	Delta        ChatMessage `json:"delta"` // For streaming responses
	FinishReason string      `json:"finish_reason"`
}

// openaiUsage represents token usage in the OpenAI response.
type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// openaiChatResponse represents the OpenAI chat completion response format.
type openaiChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openaiChatChoice `json:"choices"`
	Usage   openaiUsage        `json:"usage"`
}

// Chat sends a chat completion request and returns the response.
func (c *openaiCompatClient) Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error) {
	reqBody := openaiChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}
	if opts != nil {
		reqBody.MaxTokens = opts.MaxTokens
		reqBody.Temperature = opts.Temperature
		reqBody.Stop = opts.Stop
	}
	if reqBody.MaxTokens == 0 {
		reqBody.MaxTokens = 512
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, err
	}

	chatPath := "/v1/chat/completions"
	if c.chatPath != "" {
		chatPath = c.chatPath
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "Chat", respBody)
	}

	var chatResp openaiChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return c.convertResponse(&chatResp), nil
}

// convertResponse converts OpenAI response format to unified ChatResponse.
func (c *openaiCompatClient) convertResponse(resp *openaiChatResponse) *ChatResponse {
	result := &ChatResponse{
		Model: resp.Model,
		TokensUsed: TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
	if len(resp.Choices) > 0 {
		result.Content = resp.Choices[0].Message.Content
		result.FinishReason = resp.Choices[0].FinishReason
	}
	return result
}

// ChatStream sends a streaming chat completion request.
// The callback is invoked for each chunk of the response content.
// Tool-call support not yet implemented; returns nil tool-calls.
func (c *openaiCompatClient) ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error) {
	reqBody := openaiChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}
	if opts != nil {
		reqBody.MaxTokens = opts.MaxTokens
		reqBody.Temperature = opts.Temperature
		reqBody.Stop = opts.Stop
	}
	if reqBody.MaxTokens == 0 {
		reqBody.MaxTokens = 512
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, err
	}

	chatPath := "/v1/chat/completions"
	if c.chatPath != "" {
		chatPath = c.chatPath
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, c.handleError(resp, "ChatStream", respBody)
	}

	// Parse SSE stream with "data: {...}" format
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), streamScannerBufferMax)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Handle data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for stream end marker
			if data == "[DONE]" {
				break
			}

			var chunk openaiChatResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // Skip malformed chunks
			}

			// Extract content from delta
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				if err := callback(chunk.Choices[0].Delta.Content); err != nil {
					return nil, err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}

	return nil, nil
}

// openaiEmbeddingRequest represents the OpenAI embedding request format.
type openaiEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// openaiEmbeddingData represents a single embedding in the response.
type openaiEmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// openaiEmbeddingResponse represents the OpenAI embedding response format.
type openaiEmbeddingResponse struct {
	Object string                `json:"object"`
	Data   []openaiEmbeddingData `json:"data"`
	Model  string                `json:"model"`
	Usage  openaiUsage           `json:"usage"`
}

// Embed generates embeddings for the given texts using the /v1/embeddings endpoint.
func (c *openaiCompatClient) Embed(ctx context.Context, model string, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := openaiEmbeddingRequest{
		Model: model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "Embed", respBody)
	}

	var embedResp openaiEmbeddingResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Sort embeddings by index and extract vectors
	result := make([][]float64, len(texts))
	for _, data := range embedResp.Data {
		if data.Index < len(result) {
			result[data.Index] = data.Embedding
		}
	}

	return result, nil
}

// handleError parses an OpenAI-compatible error response and returns
// an EndpointError wrapping the typed sentinel for the status code.
// Mirrors the anthropic adapter's pattern.
func (c *openaiCompatClient) handleError(resp *http.Response, op string, body []byte) error {
	var apiMsg string
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		apiMsg = errResp.Error.Message
	}

	sent := httpStatusToSentinel(resp.StatusCode)
	if sent == nil {
		if apiMsg != "" {
			return NewEndpointError(c.baseURL, c.provider, op,
				fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, c.provider, op,
			fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, c.provider, op,
			fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, c.provider, op, sent)
}
