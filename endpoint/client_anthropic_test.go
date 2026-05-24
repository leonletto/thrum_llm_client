package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicClient_ProviderName(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com", "test-key")
	if name := client.ProviderName(); name != "anthropic" {
		t.Errorf("expected provider name 'anthropic', got %q", name)
	}
}

func TestConvertToAnthropicFormat(t *testing.T) {
	tests := []struct {
		name          string
		messages      []ChatMessage
		wantSystem    string
		wantMsgCount  int
		wantFirstRole string
	}{
		{
			name: "extracts system message",
			messages: []ChatMessage{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: "Hello"},
			},
			wantSystem:    "You are a helpful assistant.",
			wantMsgCount:  1,
			wantFirstRole: "user",
		},
		{
			name: "multiple system messages joined",
			messages: []ChatMessage{
				{Role: "system", Content: "Rule 1"},
				{Role: "system", Content: "Rule 2"},
				{Role: "user", Content: "Hi"},
			},
			wantSystem:    "Rule 1\n\nRule 2",
			wantMsgCount:  1,
			wantFirstRole: "user",
		},
		{
			name: "no system message",
			messages: []ChatMessage{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			},
			wantSystem:    "",
			wantMsgCount:  2,
			wantFirstRole: "user",
		},
		{
			name: "mixed conversation",
			messages: []ChatMessage{
				{Role: "system", Content: "Be helpful"},
				{Role: "user", Content: "What is 2+2?"},
				{Role: "assistant", Content: "4"},
				{Role: "user", Content: "Thanks!"},
			},
			wantSystem:    "Be helpful",
			wantMsgCount:  3,
			wantFirstRole: "user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system, msgs := convertToAnthropicFormat(tt.messages)

			if system != tt.wantSystem {
				t.Errorf("system = %q, want %q", system, tt.wantSystem)
			}
			if len(msgs) != tt.wantMsgCount {
				t.Errorf("message count = %d, want %d", len(msgs), tt.wantMsgCount)
			}
			if len(msgs) > 0 && msgs[0].Role != tt.wantFirstRole {
				t.Errorf("first message role = %q, want %q", msgs[0].Role, tt.wantFirstRole)
			}
			// Verify content is always array of blocks
			for _, msg := range msgs {
				if len(msg.Content) == 0 {
					t.Error("message content should not be empty")
				}
				if msg.Content[0].Type != "text" {
					t.Errorf("content block type = %q, want 'text'", msg.Content[0].Type)
				}
			}
		})
	}
}

func TestAnthropicClient_Chat(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or incorrect x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing or incorrect anthropic-version header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}

		// Verify request body
		var req AnthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "claude-3-sonnet" {
			t.Errorf("model = %q, want 'claude-3-sonnet'", req.Model)
		}
		if req.System != "Be helpful" {
			t.Errorf("system = %q, want 'Be helpful'", req.System)
		}
		if req.MaxTokens != 1000 {
			t.Errorf("max_tokens = %d, want 1000", req.MaxTokens)
		}

		// Return mock response
		resp := AnthropicResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContentBlock{
				{Type: "text", Text: "Hello! How can I help you?"},
			},
			Model:      "claude-3-sonnet-20240229",
			StopReason: "end_turn",
			Usage:      AnthropicUsage{InputTokens: 10, OutputTokens: 8},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	messages := []ChatMessage{
		{Role: "system", Content: "Be helpful"},
		{Role: "user", Content: "Hi there"},
	}
	opts := &ChatOptions{MaxTokens: 1000}

	resp, err := client.Chat(context.Background(), "claude-3-sonnet", messages, opts)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("content = %q, want 'Hello! How can I help you?'", resp.Content)
	}
	if resp.TokensUsed.PromptTokens != 10 {
		t.Errorf("prompt tokens = %d, want 10", resp.TokensUsed.PromptTokens)
	}
}

func TestAnthropicClient_ChatWithThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req AnthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		// Verify thinking config
		if req.Thinking == nil {
			t.Fatal("expected thinking config to be set")
		}
		if req.Thinking.Type != "enabled" {
			t.Errorf("thinking.type = %q, want 'enabled'", req.Thinking.Type)
		}
		if req.Thinking.BudgetTokens != 5000 {
			t.Errorf("thinking.budget_tokens = %d, want 5000", req.Thinking.BudgetTokens)
		}

		// Return response with thinking block
		resp := AnthropicResponse{
			ID:   "msg_456",
			Type: "message",
			Role: "assistant",
			Content: []AnthropicContentBlock{
				{Type: "thinking", Thinking: "Let me think about this..."},
				{Type: "text", Text: "The answer is 42."},
			},
			Model:      "claude-3-sonnet",
			StopReason: "end_turn",
			Usage:      AnthropicUsage{InputTokens: 20, OutputTokens: 50},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	messages := []ChatMessage{{Role: "user", Content: "What is the meaning of life?"}}
	opts := &ChatOptions{EnableThinking: true, ThinkingBudget: 5000}

	resp, err := client.Chat(context.Background(), "claude-3-sonnet", messages, opts)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "The answer is 42." {
		t.Errorf("content = %q, want 'The answer is 42.'", resp.Content)
	}
	if resp.Thinking != "Let me think about this..." {
		t.Errorf("thinking = %q, want 'Let me think about this...'", resp.Thinking)
	}
}

func TestAnthropicClient_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req AnthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if !req.Stream {
			t.Error("expected stream = true")
		}

		// Write SSE events
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" World"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop"}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
		}

		for _, event := range events {
			w.Write([]byte(event + "\n"))
		}
	}))
	defer server.Close()

	client := NewAnthropicClient(server.URL, "test-key")

	var chunks []string
	callback := func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	}

	messages := []ChatMessage{{Role: "user", Content: "Say hello"}}
	_, err := client.ChatStream(context.Background(), "claude-3-sonnet", messages, nil, callback)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	combined := strings.Join(chunks, "")
	if combined != "Hello World" {
		t.Errorf("combined chunks = %q, want 'Hello World'", combined)
	}
}

func TestAnthropicClient_ErrorHandling(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`,
			wantErr:    "Invalid API key",
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`,
			wantErr:    "Rate limit exceeded",
		},
		{
			name:       "API error with message",
			statusCode: http.StatusBadRequest,
			body:       `{"type":"error","error":{"type":"invalid_request_error","message":"Invalid model"}}`,
			wantErr:    "Invalid model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewAnthropicClient(server.URL, "test-key")
			_, err := client.Chat(context.Background(), "claude-3", []ChatMessage{{Role: "user", Content: "Hi"}}, nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAnthropicClient_Embed(t *testing.T) {
	client := NewAnthropicClient("https://api.anthropic.com", "test-key")
	_, err := client.Embed(context.Background(), "model", []string{"text"})
	if err == nil {
		t.Fatal("expected error for unsupported embeddings")
	}
	if !strings.Contains(err.Error(), "does not support embeddings") {
		t.Errorf("error = %q, should mention embeddings not supported", err.Error())
	}
}

func TestAnthropicClient_ContentBlockFormat(t *testing.T) {
	// Test that messages are always formatted with content blocks
	messages := []ChatMessage{
		{Role: "user", Content: "Test message"},
	}

	_, anthropicMsgs := convertToAnthropicFormat(messages)

	if len(anthropicMsgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(anthropicMsgs))
	}

	msg := anthropicMsgs[0]
	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	block := msg.Content[0]
	if block.Type != "text" {
		t.Errorf("block type = %q, want 'text'", block.Type)
	}
	if block.Text != "Test message" {
		t.Errorf("block text = %q, want 'Test message'", block.Text)
	}
}

func TestAnthropic_ErrorSentinels(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantSent error
	}{
		{"400_bad_request", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"missing field"}}`, ErrBadRequest},
		{"401_auth_with_body", 401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, ErrAuthenticationRequired},
		{"403_forbidden", 403, `{"type":"error","error":{"type":"permission_error","message":"no access"}}`, ErrForbidden},
		{"404_not_found", 404, `{"type":"error","error":{"type":"not_found_error","message":"unknown model"}}`, ErrNotFound},
		{"408_timeout", 408, ``, ErrTimeout},
		{"422_bad_request", 422, ``, ErrBadRequest},
		{"429_rate_limited_with_body", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, ErrRateLimited},
		{"500_server_error", 500, ``, ErrServerError},
		{"502_server_error", 502, ``, ErrServerError},
		{"503_unavailable", 503, ``, ErrServiceUnavailable},
		{"504_timeout", 504, ``, ErrTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			}))
			defer srv.Close()
			client := NewAnthropicClient(srv.URL, "test-key")
			_, err := client.Chat(context.Background(), "claude-test", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantSent) {
				t.Errorf("got %v; want errors.Is == %v", err, tc.wantSent)
			}
		})
	}
}

func TestAnthropic_ChatStream_OpSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	client := NewAnthropicClient(srv.URL, "test-key")
	_, err := client.ChatStream(context.Background(), "claude-test",
		[]ChatMessage{{Role: "user", Content: "hi"}}, &ChatOptions{},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	var ee *EndpointError
	if !errors.As(err, &ee) {
		t.Fatalf("want *EndpointError, got %T", err)
	}
	if ee.Op != "ChatStream" {
		t.Errorf("op=%q; want ChatStream", ee.Op)
	}
}
