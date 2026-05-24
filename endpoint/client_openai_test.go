package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOpenAICompatClient(t *testing.T) {
	client := NewOpenAICompatClient("http://localhost:8080/", "test-api-key", "", nil, "")

	if client.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL without trailing slash, got %s", client.baseURL)
	}
	if client.apiKey != "test-api-key" {
		t.Errorf("expected apiKey 'test-api-key', got %s", client.apiKey)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
}

func TestOpenAICompatClient_ProviderName(t *testing.T) {
	client := NewOpenAICompatClient("http://localhost:8080", "", "", nil, "")
	if client.ProviderName() != "openai-compat" {
		t.Errorf("expected 'openai-compat', got %s", client.ProviderName())
	}
}

func TestOpenAICompatClient_Chat(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Parse request body
		var req openaiChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false for non-streaming request")
		}

		// Send response
		resp := openaiChatResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: 1677652288,
			Model:   "gpt-4",
			Choices: []openaiChatChoice{
				{
					Index:        0,
					Message:      ChatMessage{Role: "assistant", Content: "Hello! How can I help you?"},
					FinishReason: "stop",
				},
			},
			Usage: openaiUsage{
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	ctx := context.Background()

	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	opts := &ChatOptions{MaxTokens: 100, Temperature: 0.7}

	resp, err := client.Chat(ctx, "gpt-4", messages, opts)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Hello! How can I help you?" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("unexpected model: %s", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish reason: %s", resp.FinishReason)
	}
	if resp.TokensUsed.TotalTokens != 18 {
		t.Errorf("unexpected total tokens: %d", resp.TokensUsed.TotalTokens)
	}
}

func TestOpenAICompatClient_Chat_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header, got %s", r.Header.Get("Authorization"))
		}
		resp := openaiChatResponse{
			Model:   "llama2",
			Choices: []openaiChatChoice{{Message: ChatMessage{Content: "Hi"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "", "", nil, "")
	resp, err := client.Chat(context.Background(), "llama2", []ChatMessage{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "Hi" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestOpenAICompatClient_Chat_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request"}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	_, err := client.Chat(context.Background(), "gpt-4", []ChatMessage{{Role: "user", Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for bad request")
	}
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("expected errors.Is == ErrBadRequest, got: %v", err)
	}
}

func TestOpenAICompatClient_ChatStream(t *testing.T) {
	// Create mock SSE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		// Parse request to verify stream=true
		var req openaiChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if !req.Stream {
			t.Error("expected stream=true for streaming request")
		}

		// Send SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`{"id":"1","choices":[{"delta":{"content":"Hello"}}]}`,
			`{"id":"2","choices":[{"delta":{"content":" world"}}]}`,
			`{"id":"3","choices":[{"delta":{"content":"!"}}]}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	ctx := context.Background()

	var result strings.Builder
	_, err := client.ChatStream(ctx, "gpt-4", []ChatMessage{{Role: "user", Content: "Hi"}}, nil, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	expected := "Hello world!"
	if result.String() != expected {
		t.Errorf("expected %q, got %q", expected, result.String())
	}
}

func TestOpenAICompatClient_ChatStream_CallbackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"Hi"}}]}`)
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	expectedErr := fmt.Errorf("callback error")

	_, err := client.ChatStream(context.Background(), "gpt-4", []ChatMessage{{Role: "user", Content: "Hi"}}, nil, func(chunk string) error {
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("expected callback error, got %v", err)
	}
}

func TestOpenAICompatClient_ChatStream_SSEComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Include SSE comments and empty lines
		fmt.Fprintf(w, ": this is a comment\n")
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"Test"}}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	var result strings.Builder
	_, err := client.ChatStream(context.Background(), "gpt-4", []ChatMessage{{Role: "user", Content: "Hi"}}, nil, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if result.String() != "Test" {
		t.Errorf("expected 'Test', got %q", result.String())
	}
}

func TestOpenAICompatClient_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected path /v1/embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %s", r.Header.Get("Authorization"))
		}

		var req openaiEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Model != "text-embedding-ada-002" {
			t.Errorf("expected model text-embedding-ada-002, got %s", req.Model)
		}
		if len(req.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(req.Input))
		}

		resp := openaiEmbeddingResponse{
			Object: "list",
			Data: []openaiEmbeddingData{
				{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2, 0.3}},
				{Object: "embedding", Index: 1, Embedding: []float64{0.4, 0.5, 0.6}},
			},
			Model: "text-embedding-ada-002",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "test-key", "", nil, "")
	ctx := context.Background()

	embeddings, err := client.Embed(ctx, "text-embedding-ada-002", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(embeddings))
	}
	if len(embeddings[0]) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(embeddings[0]))
	}
	if embeddings[0][0] != 0.1 {
		t.Errorf("expected first value 0.1, got %f", embeddings[0][0])
	}
	if embeddings[1][0] != 0.4 {
		t.Errorf("expected second embedding first value 0.4, got %f", embeddings[1][0])
	}
}

func TestOpenAICompatClient_Embed_EmptyInput(t *testing.T) {
	client := NewOpenAICompatClient("http://localhost:8080", "test-key", "", nil, "")
	embeddings, err := client.Embed(context.Background(), "test-model", []string{})
	if err != nil {
		t.Fatalf("Embed with empty input failed: %v", err)
	}
	if embeddings != nil {
		t.Errorf("expected nil for empty input, got %v", embeddings)
	}
}

func TestOpenAICompatClient_Embed_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(server.URL, "bad-key", "", nil, "")
	_, err := client.Embed(context.Background(), "test-model", []string{"hello"})
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Errorf("expected errors.Is == ErrAuthenticationRequired, got: %v", err)
	}
}

// TestOpenAICompatClient_ImplementsChatClientAdapter verifies interface compliance
func TestOpenAICompatClient_ImplementsChatClientAdapter(t *testing.T) {
	var _ ChatClientAdapter = (*openaiCompatClient)(nil)
}

func TestOpenAI_StreamLargeLineExceedsDefaultBuffer(t *testing.T) {
	bigContent := strings.Repeat("x", 100*1024) // 100 KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", bigContent)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		Provider:    "openai",
		APIKey:      "k",
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	var got string
	_, err = client.ChatStream(context.Background(), "m", []ChatMessage{{Role: "user", Content: "hi"}}, nil, func(chunk string) error {
		got += chunk
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if len(got) != len(bigContent) {
		t.Errorf("expected full %d-byte chunk, got %d bytes", len(bigContent), len(got))
	}
}

func TestOpenAI_ErrorSentinels(t *testing.T) {
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
		{"502", 502, ErrServerError},
		{"503", 503, ErrServiceUnavailable},
		{"504", 504, ErrTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream said no"}}`))
			}))
			defer srv.Close()
			client := NewOpenAICompatClient(srv.URL, "test", "", nil, "openai")
			_, err := client.Chat(context.Background(), "gpt-test", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantSent) {
				t.Errorf("got %v; want errors.Is == %v", err, tc.wantSent)
			}
		})
	}
}
