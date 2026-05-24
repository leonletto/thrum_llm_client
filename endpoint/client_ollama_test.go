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

func TestNewOllamaClient(t *testing.T) {
	client := NewOllamaClient("http://localhost:11434", "")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.ProviderName() != "ollama" {
		t.Errorf("expected provider name 'ollama', got %s", client.ProviderName())
	}
}

func TestOllamaClient_Chat(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Decode request body
		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "qwen3:8b" {
			t.Errorf("expected model 'qwen3:8b', got %s", req.Model)
		}
		if req.Stream != false {
			t.Errorf("expected stream false, got %v", req.Stream)
		}
		if len(req.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(req.Messages))
		}

		// Send response
		resp := ollamaChatResponse{
			Model:           "qwen3:8b",
			Done:            true,
			DoneReason:      "stop",
			Message:         ollamaChatMessage{Role: "assistant", Content: "4"},
			PromptEvalCount: 10,
			EvalCount:       5,
			EvalDuration:    100000000, // 100ms in ns
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	resp, err := client.Chat(context.Background(), "qwen3:8b", []ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	}, nil)

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "4" {
		t.Errorf("expected content '4', got %s", resp.Content)
	}
	if resp.Model != "qwen3:8b" {
		t.Errorf("expected model 'qwen3:8b', got %s", resp.Model)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %s", resp.FinishReason)
	}
	if resp.TokensUsed.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens 10, got %d", resp.TokensUsed.PromptTokens)
	}
	if resp.TokensUsed.CompletionTokens != 5 {
		t.Errorf("expected completion_tokens 5, got %d", resp.TokensUsed.CompletionTokens)
	}
}

func TestOllamaClient_Chat_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify options were applied
		if req.Options == nil {
			t.Error("expected options to be set")
		} else {
			if req.Options.NumPredict != 100 {
				t.Errorf("expected num_predict 100, got %d", req.Options.NumPredict)
			}
			if req.Options.Temperature != 0.7 {
				t.Errorf("expected temperature 0.7, got %f", req.Options.Temperature)
			}
		}

		resp := ollamaChatResponse{Model: "test", Done: true, Message: ollamaChatMessage{Content: "ok"}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	_, err := client.Chat(context.Background(), "test", []ChatMessage{
		{Role: "user", Content: "hi"},
	}, &ChatOptions{MaxTokens: 100, Temperature: 0.7})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
}

func TestOllamaClient_ChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}

		var req ollamaChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream true")
		}

		// Send streaming response (newline-delimited JSON)
		flusher, _ := w.(http.Flusher)
		chunks := []ollamaChatResponse{
			{Message: ollamaChatMessage{Content: "Hello"}, Done: false},
			{Message: ollamaChatMessage{Content: " world"}, Done: false},
			{Message: ollamaChatMessage{Content: "!"}, Done: true, DoneReason: "stop"},
		}
		for _, chunk := range chunks {
			json.NewEncoder(w).Encode(chunk)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	var result strings.Builder

	_, err := client.ChatStream(context.Background(), "test", []ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})

	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if result.String() != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %s", result.String())
	}
}

func TestOllamaClient_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}

		var req ollamaEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model 'nomic-embed-text', got %s", req.Model)
		}

		// Return mock embedding
		resp := ollamaEmbedResponse{
			Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	embeddings, err := client.Embed(context.Background(), "nomic-embed-text", []string{"hello", "world"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}
	if len(embeddings[0]) != 5 {
		t.Errorf("expected embedding dimension 5, got %d", len(embeddings[0]))
	}
}

func TestOllamaClient_Chat_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	_, err := client.Chat(context.Background(), "test", []ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, ErrServerError) {
		t.Errorf("expected errors.Is == ErrServerError, got %s", err.Error())
	}
}

func TestOllamaClient_ChatStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	_, err := client.ChatStream(context.Background(), "test", []ChatMessage{
		{Role: "user", Content: "hi"},
	}, nil, func(chunk string) error { return nil })

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("expected errors.Is == ErrBadRequest, got %s", err.Error())
	}
}

func TestOllamaClient_Embed_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL, "")
	_, err := client.Embed(context.Background(), "nonexistent", []string{"hello"})

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected errors.Is == ErrNotFound, got %s", err.Error())
	}
}

// TestOllamaClient_InterfaceCompliance verifies the client implements ChatClientAdapter
func TestOllamaClient_InterfaceCompliance(t *testing.T) {
	var _ ChatClientAdapter = (*ollamaClient)(nil)
	var _ ChatClientAdapter = NewOllamaClient("http://localhost:11434", "")
}

func TestOllama_ErrorSentinels(t *testing.T) {
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
				_, _ = w.Write([]byte(`{"error":"ollama rejected"}`))
			}))
			defer srv.Close()
			client := NewOllamaClient(srv.URL, "")
			_, err := client.Chat(context.Background(), "llama-test", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantSent) {
				t.Errorf("got %v; want errors.Is == %v", err, tc.wantSent)
			}
		})
	}
}
