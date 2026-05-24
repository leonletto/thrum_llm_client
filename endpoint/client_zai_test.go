package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZaiClient_ProviderName(t *testing.T) {
	client := NewZaiClient("test-key")
	if client.ProviderName() != "zai" {
		t.Errorf("ProviderName() = %q, want %q", client.ProviderName(), "zai")
	}
}

func TestZaiClient_Chat_ThinkingDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != ZaiChatPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, ZaiChatPath)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Auth = %q, want %q", r.Header.Get("Authorization"), "Bearer test-key")
		}

		// Parse request body
		var req ZaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Default ReasoningMode is Off — the wire body MUST send
		// {"thinking":{"type":"disabled"}} explicitly so reasoning
		// models like GLM-5.1 don't interpret an absent field as
		// thinking-ON. (Non-reasoning models that reject explicit
		// "disabled" opt into ReasoningModeAuto via the registry.)
		if req.Thinking == nil || req.Thinking.Type != "disabled" {
			t.Errorf("Thinking = %+v, want {type:disabled}", req.Thinking)
		}

		// Return response
		resp := ZaiResponse{
			ID:    "test-id",
			Model: req.Model,
			Choices: []ZaiChoice{
				{
					Index:        0,
					Message:      ZaiRespMsg{Role: "assistant", Content: "4"},
					FinishReason: "stop",
				},
			},
			Usage: ZaiUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	resp, err := client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	}, nil)

	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "4" {
		t.Errorf("Content = %q, want %q", resp.Content, "4")
	}
	if resp.Thinking != "" {
		t.Errorf("Thinking = %q, want empty", resp.Thinking)
	}
	if resp.TokensUsed.TotalTokens != 12 {
		t.Errorf("TotalTokens = %d, want 12", resp.TokensUsed.TotalTokens)
	}
}

func TestZaiClient_Chat_ThinkingEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ZaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		// Verify thinking is enabled
		if req.Thinking == nil || req.Thinking.Type != "enabled" {
			t.Errorf("Thinking = %+v, want enabled", req.Thinking)
		}

		// Return response with reasoning_content
		resp := ZaiResponse{
			ID:    "test-id",
			Model: req.Model,
			Choices: []ZaiChoice{
				{
					Index: 0,
					Message: ZaiRespMsg{
						Role:             "assistant",
						Content:          "4",
						ReasoningContent: "To add 2+2, I add the numbers together: 2+2=4",
					},
					FinishReason: "stop",
				},
			},
			Usage: ZaiUsage{PromptTokens: 10, CompletionTokens: 50, TotalTokens: 60},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	resp, err := client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	}, &ChatOptions{EnableThinking: true})

	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "4" {
		t.Errorf("Content = %q, want %q", resp.Content, "4")
	}
	if resp.Thinking == "" {
		t.Error("Thinking is empty, want reasoning content")
	}
	if !strings.Contains(resp.Thinking, "2+2=4") {
		t.Errorf("Thinking = %q, want to contain '2+2=4'", resp.Thinking)
	}
}

func TestZaiClient_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Stream SSE chunks
		chunks := []string{
			`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`data: {"id":"2","choices":[{"index":0,"delta":{"content":" World"}}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n"))
		}
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	var result strings.Builder
	_, err := client.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "Say hello"},
	}, nil, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})

	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if result.String() != "Hello World" {
		t.Errorf("Result = %q, want %q", result.String(), "Hello World")
	}
}

func TestZaiClient_ChatStream_WithThinking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Stream with reasoning_content
		chunks := []string{
			`data: {"id":"1","choices":[{"index":0,"delta":{"reasoning_content":"Thinking..."}}]}`,
			`data: {"id":"2","choices":[{"index":0,"delta":{"content":"Answer"}}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n"))
		}
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	var result strings.Builder
	_, err := client.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "test"},
	}, &ChatOptions{EnableThinking: true}, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})

	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if result.String() != "Thinking...Answer" {
		t.Errorf("Result = %q, want %q", result.String(), "Thinking...Answer")
	}
}

func TestBuildVisionMessage(t *testing.T) {
	msg := BuildVisionMessage("user", "What is this?", "data:image/png;base64,abc123")

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}

	blocks, ok := msg.Content.([]ZaiContentBlock)
	if !ok {
		t.Fatalf("Content is not []ZaiContentBlock, got %T", msg.Content)
	}

	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2", len(blocks))
	}

	if blocks[0].Type != "text" || blocks[0].Text != "What is this?" {
		t.Errorf("blocks[0] = %+v, want text block", blocks[0])
	}

	if blocks[1].Type != "image_url" || blocks[1].ImageURL == nil {
		t.Errorf("blocks[1] = %+v, want image_url block", blocks[1])
	}

	if blocks[1].ImageURL.URL != "data:image/png;base64,abc123" {
		t.Errorf("ImageURL = %q, want data:image/png;base64,abc123", blocks[1].ImageURL.URL)
	}
}

func TestBuildVisionMessageMultiImage(t *testing.T) {
	urls := []string{"https://example.com/img1.png", "https://example.com/img2.png"}
	msg := BuildVisionMessageMultiImage("user", "Compare these", urls)

	blocks, ok := msg.Content.([]ZaiContentBlock)
	if !ok {
		t.Fatalf("Content is not []ZaiContentBlock, got %T", msg.Content)
	}

	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (1 text + 2 images)", len(blocks))
	}

	if blocks[0].Type != "text" {
		t.Errorf("blocks[0].Type = %q, want text", blocks[0].Type)
	}

	for i, url := range urls {
		if blocks[i+1].ImageURL.URL != url {
			t.Errorf("blocks[%d].ImageURL.URL = %q, want %q", i+1, blocks[i+1].ImageURL.URL, url)
		}
	}
}

func TestZaiClient_Chat_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid model"}`))
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	_, err := client.Chat(context.Background(), "invalid-model", []ChatMessage{
		{Role: "user", Content: "test"},
	}, nil)

	if err == nil {
		t.Fatal("Chat() error = nil, want error")
	}
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("Error = %q, want errors.Is == ErrBadRequest", err.Error())
	}
}

func TestZaiClient_Embed_NotSupported(t *testing.T) {
	client := NewZaiClient("test-key")
	_, err := client.Embed(context.Background(), "model", []string{"text"})

	if err == nil {
		t.Fatal("Embed() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("Error = %q, want to contain 'not supported'", err.Error())
	}
}

func TestZaiClient_DefaultMaxTokens(t *testing.T) {
	var capturedMaxTokens int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ZaiRequest
		json.NewDecoder(r.Body).Decode(&req)
		capturedMaxTokens = req.MaxTokens

		resp := ZaiResponse{
			ID:      "test",
			Choices: []ZaiChoice{{Message: ZaiRespMsg{Content: "ok"}}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	// Don't specify MaxTokens
	client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "test"},
	}, nil)

	if capturedMaxTokens != 8000 {
		t.Errorf("MaxTokens = %d, want 8000 (default)", capturedMaxTokens)
	}
}

func TestZaiBuildThinking_Matrix(t *testing.T) {
	cases := []struct {
		name     string
		mode     ReasoningMode
		enabled  bool
		wantPtr  bool
		wantType string
	}{
		{"off+disabled", ReasoningModeOff, false, true, "disabled"},
		{"off+enabled", ReasoningModeOff, true, true, "enabled"},
		{"auto+disabled", ReasoningModeAuto, false, false, ""},
		{"auto+enabled", ReasoningModeAuto, true, true, "enabled"},
		{"on+disabled", ReasoningModeOn, false, true, "enabled"},
		{"on+enabled", ReasoningModeOn, true, true, "enabled"},
	}
	c := &ZaiClient{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.buildThinkingFor(tc.enabled, tc.mode)
			if tc.wantPtr && got == nil {
				t.Fatalf("got nil; want non-nil with type=%q", tc.wantType)
			}
			if !tc.wantPtr && got != nil {
				t.Fatalf("got %+v; want nil", got)
			}
			if got != nil && got.Type != tc.wantType {
				t.Errorf("got Type=%q; want %q", got.Type, tc.wantType)
			}
		})
	}
}

func TestZaiBuildThinking_DefaultZeroMode_RestoresExplicitDisabled(t *testing.T) {
	c := &ZaiClient{}
	got := c.buildThinkingFor(false, ReasoningModeOff)
	if got == nil {
		t.Fatal("got nil; regression: must emit explicit disabled")
	}
	if got.Type != "disabled" {
		t.Fatalf("got Type=%q; want %q", got.Type, "disabled")
	}
}

func TestZaiClient_WireBody_DefaultDisabled(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"glm-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(context.Background(), "glm-5.1", []ChatMessage{{Role: "user", Content: "hi"}}, &ChatOptions{}); err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("body unmarshal: %v (raw=%s)", err, captured)
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected body.thinking object, got body=%v", body)
	}
	if thinking["type"] != "disabled" {
		t.Errorf("body.thinking.type = %v; want %q", thinking["type"], "disabled")
	}
}

func TestZaiClient_WireBody_AutoMode_OmitsField(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"nrm-v1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	reg := NewEmptyModelRegistry()
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "nrm",
		ProviderModels: map[string]string{"zai": "nrm-v1"},
		ReasoningMode:  ReasoningModeAuto,
	}); err != nil {
		t.Fatal(err)
	}

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL:   srv.URL,
		Provider:      "zai",
		APIKey:        "test",
		ModelRegistry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(context.Background(), "nrm", []ChatMessage{{Role: "user", Content: "hi"}}, &ChatOptions{}); err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("body unmarshal: %v (raw=%s)", err, captured)
	}
	if _, present := body["thinking"]; present {
		t.Errorf("auto mode + EnableThinking=false: body.thinking should be ABSENT, got %v", body["thinking"])
	}
}

func TestZai_ErrorSentinels(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantSent error
	}{
		{"400", 400, ErrBadRequest},
		{"401", 401, ErrAuthenticationRequired},
		{"403", 403, ErrForbidden},
		{"404", 404, ErrNotFound},
		{"408", 408, ErrTimeout},
		{"422", 422, ErrBadRequest},
		{"429", 429, ErrRateLimited},
		{"500", 500, ErrServerError},
		{"503", 503, ErrServiceUnavailable},
		{"504", 504, ErrTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"zai upstream rejected"}}`))
			}))
			defer srv.Close()
			client := NewZaiClientWithEndpoint("test-key", srv.URL)
			_, err := client.Chat(context.Background(), "glm-test", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantSent) {
				t.Errorf("got %v; want errors.Is == %v", err, tc.wantSent)
			}
		})
	}
}
