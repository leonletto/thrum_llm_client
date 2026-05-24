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

// zaiTool wraps a tool advertisement in OpenAI-compatible shape.
type zaiTool struct {
	Type     string          `json:"type"` // always "function"
	Function zaiToolFunction `json:"function"`
}

type zaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// zaiToolCall is an inbound tool-call from the assistant.
type zaiToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"` // "function"
	Function zaiToolCallFunction `json:"function"`
}

type zaiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-stringified per OpenAI convention
}

// zaiToolCallDelta is the streaming delta for a tool call.
type zaiToolCallDelta struct {
	Index    int                      `json:"index"`
	ID       string                   `json:"id,omitempty"`
	Type     string                   `json:"type,omitempty"`
	Function zaiToolCallFunctionDelta `json:"function"`
}

type zaiToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // accumulates across deltas
}

// ZaiClient implements ChatClientAdapter for Z.ai's GLM models.
// Supports thinking control and vision models with content blocks.
type ZaiClient struct {
	httpClient            *http.Client
	baseURL               string
	apiKey                string
	chatPath              string
	extraBody             map[string]any
	extraHeaders          map[string]string
	reasoningModeResolver func(model string) ReasoningMode
}

func (c *ZaiClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *ZaiClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *ZaiClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *ZaiClient) setReasoningModeResolver(f func(model string) ReasoningMode) {
	c.reasoningModeResolver = f
}

// ZaiMessage represents a message in Z.ai's chat format.
// Content can be a string for text-only or []ZaiContentBlock for vision.
type ZaiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"`                // string or []ZaiContentBlock
	ToolCalls  []zaiToolCall `json:"tool_calls,omitempty"`   // outbound tool-calls on assistant turns
	ToolCallID string        `json:"tool_call_id,omitempty"` // for role="tool" messages
}

// ZaiContentBlock represents a content block for multi-modal messages.
type ZaiContentBlock struct {
	Type     string       `json:"type"`                // "text" or "image_url"
	Text     string       `json:"text,omitempty"`      // for type="text"
	ImageURL *ZaiImageURL `json:"image_url,omitempty"` // for type="image_url"
}

// ZaiImageURL contains the image URL data.
type ZaiImageURL struct {
	URL string `json:"url"` // data:image/png;base64,... or https://...
}

// ZaiThinking controls the thinking mode for reasoning models.
type ZaiThinking struct {
	Type string `json:"type"` // "enabled" or "disabled"
}

// ZaiRequest represents a chat completion request to Z.ai.
type ZaiRequest struct {
	Model       string       `json:"model"`
	Messages    []ZaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature float64      `json:"temperature"`
	Stream      bool         `json:"stream"`
	Thinking    *ZaiThinking `json:"thinking,omitempty"`
	Tools       []zaiTool    `json:"tools,omitempty"` // only sent if non-empty
}

// ZaiResponse represents a chat completion response from Z.ai.
type ZaiResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []ZaiChoice `json:"choices"`
	Usage   ZaiUsage    `json:"usage"`
}

// ZaiChoice represents a single choice in the response.
type ZaiChoice struct {
	Index        int        `json:"index"`
	Message      ZaiRespMsg `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

// ZaiRespMsg represents the message content in a response.
type ZaiRespMsg struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []zaiToolCall `json:"tool_calls,omitempty"`
}

// ZaiUsage contains token usage information.
type ZaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ZaiStreamChunk represents a streaming chunk from Z.ai.
type ZaiStreamChunk struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []ZaiStreamDelta `json:"choices"`
}

// ZaiStreamDelta represents the delta in a streaming chunk.
type ZaiStreamDelta struct {
	Index int `json:"index"`
	Delta struct {
		Content          string             `json:"content,omitempty"`
		ReasoningContent string             `json:"reasoning_content,omitempty"`
		ToolCalls        []zaiToolCallDelta `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

const (
	// ZaiDefaultEndpoint is the Z.ai API endpoint.
	ZaiDefaultEndpoint = "https://api.z.ai"
	// ZaiChatPath is the chat completions API path.
	ZaiChatPath = "/api/coding/paas/v4/chat/completions"
)

// NewZaiClient creates a new Z.ai client adapter.
func NewZaiClient(apiKey string) *ZaiClient {
	return &ZaiClient{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		baseURL: ZaiDefaultEndpoint,
		apiKey:  apiKey,
	}
}

// NewZaiClientWithEndpoint creates a Z.ai client with a custom endpoint.
func NewZaiClientWithEndpoint(apiKey, baseURL string) *ZaiClient {
	client := NewZaiClient(apiKey)
	client.baseURL = strings.TrimSuffix(baseURL, "/")
	return client
}

// effectiveChatPath returns the configured chat path, falling back to ZaiChatPath.
func (c *ZaiClient) effectiveChatPath() string {
	if c.chatPath != "" {
		return c.chatPath
	}
	return ZaiChatPath
}

// ProviderName returns the provider identifier.
func (c *ZaiClient) ProviderName() string {
	return "zai"
}

// Chat sends a chat completion request to Z.ai.
func (c *ZaiClient) Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error) {
	if opts == nil {
		opts = &ChatOptions{}
	}

	// Convert messages to Z.ai format
	zaiMessages := c.convertMessages(messages)

	// Build request with thinking control
	req := ZaiRequest{
		Model:       model,
		Messages:    zaiMessages,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		Stream:      false,
		Thinking:    c.buildThinking(model, opts.EnableThinking),
	}

	// Set defaults
	if req.MaxTokens == 0 {
		req.MaxTokens = 8000
	}

	// Include tools if provided
	if len(opts.Tools) > 0 {
		req.Tools = make([]zaiTool, len(opts.Tools))
		for i, td := range opts.Tools {
			req.Tools[i] = zaiTool{
				Type: "function",
				Function: zaiToolFunction{
					Name:        td.Name,
					Description: td.Description,
					Parameters:  td.Schema,
				},
			}
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("zai: failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("zai: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+c.effectiveChatPath(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zai: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, c.handleError(resp, "Chat", bodyBytes)
	}

	var zaiResp ZaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&zaiResp); err != nil {
		return nil, fmt.Errorf("zai: failed to decode response: %w", err)
	}

	return c.convertResponse(&zaiResp, opts.EnableThinking)
}

// ChatStream sends a streaming chat request to Z.ai.
// Returns finalized tool-calls accumulated from streaming deltas (nil if none).
func (c *ZaiClient) ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error) {
	if opts == nil {
		opts = &ChatOptions{}
	}

	// Convert messages to Z.ai format
	zaiMessages := c.convertMessages(messages)

	// Build request with thinking control
	req := ZaiRequest{
		Model:       model,
		Messages:    zaiMessages,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		Stream:      true,
		Thinking:    c.buildThinking(model, opts.EnableThinking),
	}

	// Set defaults
	if req.MaxTokens == 0 {
		req.MaxTokens = 8000
	}

	// Include tools if provided
	if len(opts.Tools) > 0 {
		req.Tools = make([]zaiTool, len(opts.Tools))
		for i, td := range opts.Tools {
			req.Tools[i] = zaiTool{
				Type: "function",
				Function: zaiToolFunction{
					Name:        td.Name,
					Description: td.Description,
					Parameters:  td.Schema,
				},
			}
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("zai: failed to marshal request: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("zai: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+c.effectiveChatPath(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zai: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, c.handleError(resp, "ChatStream", bodyBytes)
	}

	return c.parseStream(resp.Body, opts.EnableThinking, callback)
}

// Embed generates embeddings (not supported by Z.ai).
func (c *ZaiClient) Embed(_ context.Context, _ string, _ []string) ([][]float64, error) {
	return nil, fmt.Errorf("zai: embeddings not supported")
}

// buildThinking returns the wire-shape thinking control for a single
// request. It dispatches on the per-model ReasoningMode (resolved via
// the registry-derived resolver, or ReasoningModeOff when unset):
//
//	ReasoningModeOff (default):
//	  enabled=false → {"type":"disabled"}
//	  enabled=true  → {"type":"enabled"}
//	ReasoningModeAuto (model rejects explicit disabled):
//	  enabled=false → nil (field omitted)
//	  enabled=true  → {"type":"enabled"}
//	ReasoningModeOn (always-reasoning model):
//	  any           → {"type":"enabled"}
//
// Default-zero behavior (Off) uses the explicit-disabled wire shape:
// reasoning models like GLM-5.1 receive an explicit disabled signal
// rather than an absent field that they would interpret as
// thinking-on.
func (c *ZaiClient) buildThinking(model string, enabled bool) *ZaiThinking {
	mode := ReasoningModeOff
	if c.reasoningModeResolver != nil {
		mode = c.reasoningModeResolver(model)
	}
	return c.buildThinkingFor(enabled, mode)
}

// buildThinkingFor is the pure mapping from (enabled, mode) to wire
// shape, factored out for testability.
func (c *ZaiClient) buildThinkingFor(enabled bool, mode ReasoningMode) *ZaiThinking {
	switch mode {
	case ReasoningModeOn:
		return &ZaiThinking{Type: "enabled"}
	case ReasoningModeAuto:
		if !enabled {
			return nil
		}
		return &ZaiThinking{Type: "enabled"}
	case ReasoningModeOff:
		fallthrough
	default:
		if enabled {
			return &ZaiThinking{Type: "enabled"}
		}
		return &ZaiThinking{Type: "disabled"}
	}
}

// convertMessages converts ChatMessage to ZaiMessage format.
func (c *ZaiClient) convertMessages(messages []ChatMessage) []ZaiMessage {
	zaiMessages := make([]ZaiMessage, len(messages))
	for i, msg := range messages {
		zm := ZaiMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
		// Handle assistant tool-call turns
		if len(msg.ToolCalls) > 0 {
			zm.ToolCalls = make([]zaiToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				zm.ToolCalls[j] = zaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: zaiToolCallFunction{
						Name:      tc.Name,
						Arguments: string(tc.Args), // json.RawMessage → stringified JSON
					},
				}
			}
		}
		// Handle tool-result turns
		if msg.Role == "tool" {
			zm.ToolCallID = msg.ToolCallID
		}
		zaiMessages[i] = zm
	}
	return zaiMessages
}

// convertResponse converts ZaiResponse to ChatResponse.
func (c *ZaiClient) convertResponse(resp *ZaiResponse, enableThinking bool) (*ChatResponse, error) {
	result := &ChatResponse{
		Model: resp.Model,
		TokensUsed: TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		result.Content = choice.Message.Content
		result.FinishReason = choice.FinishReason

		// Include reasoning content if thinking was enabled
		if enableThinking && choice.Message.ReasoningContent != "" {
			result.Thinking = choice.Message.ReasoningContent
		}

		// Convert tool calls
		if len(choice.Message.ToolCalls) > 0 {
			result.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
			for i, ztc := range choice.Message.ToolCalls {
				// Parse the stringified JSON arguments into json.RawMessage
				var probe interface{}
				if err := json.Unmarshal([]byte(ztc.Function.Arguments), &probe); err != nil {
					return nil, fmt.Errorf("%w: tool-call %q args not valid JSON: %s", ErrZaiEmptyToolCall, ztc.Function.Name, err.Error())
				}
				result.ToolCalls[i] = ToolCall{
					ID:   ztc.ID,
					Name: ztc.Function.Name,
					Args: json.RawMessage(ztc.Function.Arguments),
				}
			}
		}
	}

	return result, nil
}

// parseStream parses SSE stream, invokes callback for text chunks, and accumulates tool-call deltas.
// Returns finalized tool-calls after the stream ends (nil if none).
func (c *ZaiClient) parseStream(body io.Reader, enableThinking bool, callback func(chunk string) error) ([]ToolCall, error) {
	// Tool-call delta accumulators keyed by index
	toolCalls := map[int]*ToolCall{}
	toolCallOrder := []int{}
	argsAccum := map[int]*strings.Builder{}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), streamScannerBufferMax)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Check for SSE data prefix
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// Extract JSON payload
		jsonStr := strings.TrimPrefix(line, "data: ")

		// Check for stream end
		if jsonStr == "[DONE]" {
			break
		}

		// Parse chunk
		var chunk ZaiStreamChunk
		if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
			// Skip malformed chunks
			continue
		}

		// Extract content from delta
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			var text string

			// When thinking enabled, prefer reasoning_content, then content
			// When thinking disabled, prefer content, then reasoning_content
			if enableThinking {
				text = delta.ReasoningContent
				if text == "" {
					text = delta.Content
				}
			} else {
				text = delta.Content
				if text == "" {
					text = delta.ReasoningContent
				}
			}

			if text != "" {
				if err := callback(text); err != nil {
					return nil, err
				}
			}

			// Accumulate tool-call deltas
			for _, tcd := range delta.ToolCalls {
				tc, ok := toolCalls[tcd.Index]
				if !ok {
					tc = &ToolCall{}
					toolCalls[tcd.Index] = tc
					toolCallOrder = append(toolCallOrder, tcd.Index)
					argsAccum[tcd.Index] = &strings.Builder{}
				}
				if tcd.ID != "" {
					tc.ID = tcd.ID
				}
				if tcd.Function.Name != "" {
					tc.Name = tcd.Function.Name
				}
				argsAccum[tcd.Index].WriteString(tcd.Function.Arguments)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Finalize tool calls from accumulated argument strings
	if len(toolCallOrder) == 0 {
		return nil, nil
	}

	result := make([]ToolCall, len(toolCallOrder))
	for i, idx := range toolCallOrder {
		raw := argsAccum[idx].String()
		var probe interface{}
		if err := json.Unmarshal([]byte(raw), &probe); err != nil {
			return nil, fmt.Errorf("%w: tool-call %d args not valid JSON: %s", ErrZaiEmptyToolCall, idx, err.Error())
		}
		tc := toolCalls[idx]
		tc.Args = json.RawMessage(raw)
		result[i] = *tc
	}

	return result, nil
}

// BuildVisionMessage creates a ZaiMessage with text and image content blocks.
// Use this for vision model requests (e.g., GLM-4.6V).
func BuildVisionMessage(role, text, imageURL string) ZaiMessage {
	return ZaiMessage{
		Role: role,
		Content: []ZaiContentBlock{
			{Type: "text", Text: text},
			{Type: "image_url", ImageURL: &ZaiImageURL{URL: imageURL}},
		},
	}
}

// BuildVisionMessageMultiImage creates a ZaiMessage with text and multiple images.
func BuildVisionMessageMultiImage(role, text string, imageURLs []string) ZaiMessage {
	blocks := make([]ZaiContentBlock, 0, len(imageURLs)+1)
	blocks = append(blocks, ZaiContentBlock{Type: "text", Text: text})
	for _, url := range imageURLs {
		blocks = append(blocks, ZaiContentBlock{
			Type:     "image_url",
			ImageURL: &ZaiImageURL{URL: url},
		})
	}
	return ZaiMessage{
		Role:    role,
		Content: blocks,
	}
}

// handleError mirrors the anthropic/openai pattern: parse the body
// for any human-readable message, dispatch via httpStatusToSentinel
// for the typed wrap.
func (c *ZaiClient) handleError(resp *http.Response, op string, body []byte) error {
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
			return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, "zai", op, sent)
}
