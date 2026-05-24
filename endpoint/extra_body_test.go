package endpoint

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// recordingTransport wraps an underlying RoundTripper and counts calls so
// tests can assert that an injected *http.Client was actually used.
type recordingTransport struct {
	calls atomic.Int64
	inner http.RoundTripper
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls.Add(1)
	return rt.inner.RoundTrip(req)
}

func newRecordingClient() (*http.Client, *recordingTransport) {
	rt := &recordingTransport{inner: http.DefaultTransport}
	return &http.Client{Transport: rt}, rt
}

// readJSONBody parses an http.Request body into a map for assertions.
func readJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body: %v (raw=%s)", err, string(raw))
	}
	return m
}

// --- OpenAI-compat ---------------------------------------------------------

func TestOpenAICompat_HTTPClientInjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(openaiChatResponse{
			Model:   "m",
			Choices: []openaiChatChoice{{Message: ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	httpClient, rt := newRecordingClient()
	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "openai",
		APIKey:      "k",
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if rt.calls.Load() == 0 {
		t.Fatal("injected HTTPClient was not used")
	}
}

func TestOpenAICompat_ExtraBodyMerge(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = readJSONBody(t, r)
		json.NewEncoder(w).Encode(openaiChatResponse{
			Model:   "m",
			Choices: []openaiChatChoice{{Message: ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "openai",
		APIKey:      "k",
		ExtraBody: map[string]any{
			"top_k": 50,
			"model": "should-override",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "original-model", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if v, ok := captured["top_k"]; !ok || int(v.(float64)) != 50 {
		t.Errorf("top_k missing or wrong: %v", captured["top_k"])
	}
	if v, _ := captured["model"].(string); v != "should-override" {
		t.Errorf("ExtraBody must override model field, got %q", v)
	}
}

// --- Anthropic ------------------------------------------------------------

func TestAnthropic_HTTPClientInjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(AnthropicResponse{
			ID: "x", Type: "message", Role: "assistant", Model: "m",
			Content: []AnthropicContentBlock{{Type: "text", Text: "ok"}},
			Usage:   AnthropicUsage{InputTokens: 1, OutputTokens: 1},
		})
	}))
	defer server.Close()

	httpClient, rt := newRecordingClient()
	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "anthropic",
		APIKey:      "k",
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if rt.calls.Load() == 0 {
		t.Fatal("injected HTTPClient was not used")
	}
}

func TestAnthropic_ExtraBodyMerge(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = readJSONBody(t, r)
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
		ExtraBody: map[string]any{
			"top_k": 50,
			"model": "should-override",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "original-model", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if v, ok := captured["top_k"]; !ok || int(v.(float64)) != 50 {
		t.Errorf("top_k missing or wrong: %v", captured["top_k"])
	}
	if v, _ := captured["model"].(string); v != "should-override" {
		t.Errorf("ExtraBody must override model field, got %q", v)
	}
}

// --- Z.ai -----------------------------------------------------------------

func TestZai_HTTPClientInjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ZaiResponse{
			Model:   "m",
			Choices: []ZaiChoice{{Message: ZaiRespMsg{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			Usage:   ZaiUsage{TotalTokens: 1},
		})
	}))
	defer server.Close()

	httpClient, rt := newRecordingClient()
	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "zai",
		APIKey:      "k",
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if rt.calls.Load() == 0 {
		t.Fatal("injected HTTPClient was not used")
	}
}

func TestZai_ExtraBodyMerge(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = readJSONBody(t, r)
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
		ExtraBody: map[string]any{
			"top_k": 50,
			"model": "should-override",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "original-model", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if v, ok := captured["top_k"]; !ok || int(v.(float64)) != 50 {
		t.Errorf("top_k missing or wrong: %v", captured["top_k"])
	}
	if v, _ := captured["model"].(string); v != "should-override" {
		t.Errorf("ExtraBody must override model field, got %q", v)
	}
}

// --- Ollama ---------------------------------------------------------------

func TestOllama_HTTPClientInjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "m", Done: true, DoneReason: "stop",
			Message: ollamaChatMessage{Role: "assistant", Content: "ok"},
		})
	}))
	defer server.Close()

	httpClient, rt := newRecordingClient()
	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "ollama",
		HTTPClient:  httpClient,
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if rt.calls.Load() == 0 {
		t.Fatal("injected HTTPClient was not used")
	}
}

func TestOllama_ExtraBodyMerge(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = readJSONBody(t, r)
		json.NewEncoder(w).Encode(ollamaChatResponse{
			Model: "m", Done: true, DoneReason: "stop",
			Message: ollamaChatMessage{Role: "assistant", Content: "ok"},
		})
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "ollama",
		ExtraBody: map[string]any{
			"top_k": 50,
			"model": "should-override",
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), "original-model", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if v, ok := captured["top_k"]; !ok || int(v.(float64)) != 50 {
		t.Errorf("top_k missing or wrong: %v", captured["top_k"])
	}
	if v, _ := captured["model"].(string); v != "should-override" {
		t.Errorf("ExtraBody must override model field, got %q", v)
	}
}
