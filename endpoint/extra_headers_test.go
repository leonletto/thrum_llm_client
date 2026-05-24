package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureHeaders extracts a single header value and a flag indicating
// whether the test custom-header key was present.
type capturedHeaders struct {
	custom    string
	userAgent string
}

func observeHeaders(r *http.Request) capturedHeaders {
	return capturedHeaders{
		custom:    r.Header.Get("X-Custom-Test"),
		userAgent: r.Header.Get("User-Agent"),
	}
}

// --- Z.ai chat path -------------------------------------------------------

func TestZai_ExtraHeadersAppliedAndOverride(t *testing.T) {
	var got capturedHeaders
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = observeHeaders(r)
		json.NewEncoder(w).Encode(ZaiResponse{
			Model:   "m",
			Choices: []ZaiChoice{{Message: ZaiRespMsg{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			Usage:   ZaiUsage{TotalTokens: 1},
		})
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "zai",
		APIKey:      "k",
		ExtraHeaders: map[string]string{
			"X-Custom-Test": "wave-h-1",
			"User-Agent":    "override-wins",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.custom != "wave-h-1" {
		t.Errorf("custom header: got %q, want %q", got.custom, "wave-h-1")
	}
	if got.userAgent != "override-wins" {
		t.Errorf("override-wins on User-Agent: got %q, want %q", got.userAgent, "override-wins")
	}
}

// --- Z.ai stream path -----------------------------------------------------

func TestZai_ExtraHeadersAppliedOnStream(t *testing.T) {
	var got capturedHeaders
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = observeHeaders(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "zai",
		APIKey:      "k",
		ExtraHeaders: map[string]string{
			"X-Custom-Test": "stream-wave-h-1",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.ChatStream(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil, func(string) error { return nil }); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got.custom != "stream-wave-h-1" {
		t.Errorf("custom header on stream: got %q, want %q", got.custom, "stream-wave-h-1")
	}
}

// --- Anthropic ------------------------------------------------------------

func TestAnthropic_ExtraHeadersAppliedAndOverride(t *testing.T) {
	var got capturedHeaders
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = observeHeaders(r)
		json.NewEncoder(w).Encode(AnthropicResponse{
			ID: "x", Type: "message", Role: "assistant", Model: "m",
			Content: []AnthropicContentBlock{{Type: "text", Text: "ok"}},
			Usage:   AnthropicUsage{InputTokens: 1, OutputTokens: 1},
		})
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "anthropic",
		APIKey:      "k",
		ExtraHeaders: map[string]string{
			"X-Custom-Test": "anthropic-wave-h-1",
			"User-Agent":    "override-wins",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.custom != "anthropic-wave-h-1" {
		t.Errorf("custom header: got %q, want %q", got.custom, "anthropic-wave-h-1")
	}
	if got.userAgent != "override-wins" {
		t.Errorf("override-wins on User-Agent: got %q, want %q", got.userAgent, "override-wins")
	}
}

// --- Registry merge -------------------------------------------------------

func TestRegistry_ExtraHeadersMergeWithCallerOverride(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		json.NewEncoder(w).Encode(ZaiResponse{
			Model:   "m",
			Choices: []ZaiChoice{{Message: ZaiRespMsg{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			Usage:   ZaiUsage{TotalTokens: 1},
		})
	}))
	defer server.Close()

	t.Setenv("FAKE_KEY", "k")
	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:      "fake",
			Type:      "zai",
			Endpoint:  server.URL,
			APIKeyEnv: "FAKE_KEY",
			ExtraHeaders: map[string]string{
				"X-Registry-Only": "registry-value",
				"X-Both":          "registry-loses",
			},
			Capabilities: ProviderCapabilities{Streaming: true},
		},
	})

	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "fake",
		ProviderRegistry: reg,
		ExtraHeaders: map[string]string{
			"X-Caller-Only": "caller-value",
			"X-Both":        "caller-wins",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if v := got.Get("X-Registry-Only"); v != "registry-value" {
		t.Errorf("registry-only header lost: got %q", v)
	}
	if v := got.Get("X-Caller-Only"); v != "caller-value" {
		t.Errorf("caller-only header lost: got %q", v)
	}
	if v := got.Get("X-Both"); v != "caller-wins" {
		t.Errorf("override-wins violated: got %q, want %q", v, "caller-wins")
	}
}

// --- Ollama ---------------------------------------------------------------

func TestOllama_ExtraHeadersAppliedAndOverride(t *testing.T) {
	var got capturedHeaders
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = observeHeaders(r)
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "m", Done: true, DoneReason: "stop",
			Message: ollamaChatMessage{Role: "assistant", Content: "ok"},
		})
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "ollama",
		ExtraHeaders: map[string]string{
			"X-Custom-Test": "ollama-wave-h-1",
			"User-Agent":    "override-wins",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.custom != "ollama-wave-h-1" {
		t.Errorf("custom header: got %q, want %q", got.custom, "ollama-wave-h-1")
	}
	if got.userAgent != "override-wins" {
		t.Errorf("override-wins on User-Agent: got %q, want %q", got.userAgent, "override-wins")
	}
}
