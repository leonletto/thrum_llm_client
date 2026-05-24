package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewChatClient_ProviderDetection(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantProvider string
	}{
		{"anthropic domain", "https://api.anthropic.com", "anthropic"},
		{"anthropic with path", "https://api.anthropic.com/v1", "anthropic"},
		{"zai domain", "https://api.z.ai", "zai"},
		{"zai subdomain", "https://bigmodel.z.ai/api", "zai"},
		{"ollama default port", "http://localhost:11434", "ollama"},
		{"ollama alt port", "http://192.168.1.100:11445", "ollama"},
		{"openai domain", "https://api.openai.com", "openai"},
		{"vllm local", "http://localhost:8000", "openai"},
		{"deepseek api", "https://api.deepseek.com", "openai"},
		{"groq api", "https://api.groq.com", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewChatClient(ChatClientConfig{
				EndpointURL: tt.url,
				APIKey:      "test-key",
			})
			if err != nil {
				t.Fatalf("NewChatClient() error = %v", err)
			}

			got := client.ProviderName()
			// Normalize: openai-compat == openai for test purposes
			if got == "openai-compat" {
				got = "openai"
			}
			if got != tt.wantProvider {
				t.Errorf("ProviderName() = %v, want %v", got, tt.wantProvider)
			}
		})
	}
}

func TestNewChatClient_ExplicitProvider(t *testing.T) {
	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: "http://custom-endpoint:8080",
		Provider:    "anthropic",
		APIKey:      "test-key",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	if got := client.ProviderName(); got != "anthropic" {
		t.Errorf("ProviderName() = %v, want anthropic", got)
	}
}

func TestNewChatClient_MissingURL(t *testing.T) {
	_, err := NewChatClient(ChatClientConfig{
		APIKey: "test-key",
	})
	if err == nil {
		t.Error("NewChatClient() expected error for missing URL")
	}
}

func TestUnifiedChatClient_Chat(t *testing.T) {
	// Create mock OpenAI-compatible server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := openaiChatResponse{
			ID:    "test-id",
			Model: "gpt-4",
			Choices: []openaiChatChoice{
				{Index: 0, Message: ChatMessage{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"},
			},
			Usage: openaiUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		APIKey:      "test-key",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), "gpt-4", []ChatMessage{
		{Role: "user", Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if resp.Content != "Hello!" {
		t.Errorf("Chat() Content = %v, want Hello!", resp.Content)
	}
	if resp.TokensUsed.TotalTokens != 15 {
		t.Errorf("Chat() TotalTokens = %v, want 15", resp.TokensUsed.TotalTokens)
	}
}

func TestUnifiedChatClient_ChatStream(t *testing.T) {
	// Create mock streaming server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		APIKey:      "test-key",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	var result strings.Builder
	_, err = client.ChatStream(context.Background(), "gpt-4", []ChatMessage{
		{Role: "user", Content: "Hi"},
	}, nil, func(chunk string) error {
		result.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if got := result.String(); got != "Hello world" {
		t.Errorf("ChatStream() result = %v, want 'Hello world'", got)
	}
}

func TestUnifiedChatClient_Embed(t *testing.T) {
	// Create mock embeddings server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := openaiEmbeddingResponse{
			Object: "list",
			Data: []openaiEmbeddingData{
				{Object: "embedding", Index: 0, Embedding: []float64{0.1, 0.2, 0.3}},
				{Object: "embedding", Index: 1, Embedding: []float64{0.4, 0.5, 0.6}},
			},
			Model: "text-embedding-ada-002",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL: server.URL,
		APIKey:      "test-key",
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	embeddings, err := client.Embed(context.Background(), "text-embedding-ada-002", []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	if len(embeddings) != 2 {
		t.Fatalf("Embed() returned %d embeddings, want 2", len(embeddings))
	}
	if len(embeddings[0]) != 3 {
		t.Errorf("Embed()[0] has %d dimensions, want 3", len(embeddings[0]))
	}
	if embeddings[0][0] != 0.1 {
		t.Errorf("Embed()[0][0] = %v, want 0.1", embeddings[0][0])
	}
}

func TestUnifiedChatClient_ModelResolution(t *testing.T) {
	// Track which model was received by the server
	var receivedModel string

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		receivedModel = req.Model

		resp := openaiChatResponse{
			ID:      "test-id",
			Model:   req.Model,
			Choices: []openaiChatChoice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create registry with model mapping
	// The openaiCompatClient's ProviderName() returns the provider passed to createAdapter.
	// For auto-detected localhost URLs, detectProvider returns "openai".
	registry := NewEmptyModelRegistry()
	err := registry.Register(&ModelProfile{
		CanonicalID: "test-model-canonical",
		DisplayName: "Test Model",
		ProviderModels: map[string]string{
			"openai": "test-model-provider-specific",
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client, err := NewChatClient(ChatClientConfig{
		EndpointURL:   server.URL,
		APIKey:        "test-key",
		ModelRegistry: registry,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	// Use canonical model name - should be resolved to provider-specific name
	_, err = client.Chat(context.Background(), "test-model-canonical", []ChatMessage{
		{Role: "user", Content: "test"},
	}, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	// Verify model was resolved
	wantModel := "test-model-provider-specific"
	if receivedModel != wantModel {
		t.Errorf("model not resolved: got %v, want %v", receivedModel, wantModel)
	}
}

func TestUnifiedChatClient_ImplementsInterface(t *testing.T) {
	// Compile-time check is in chat_client.go via var _ ChatClient = (*UnifiedChatClient)(nil)
	// This test verifies at runtime as well
	var client interface{} = &UnifiedChatClient{}
	if _, ok := client.(ChatClient); !ok {
		t.Error("UnifiedChatClient does not implement ChatClient interface")
	}
}

func TestNewChatClient_ProviderRegistry(t *testing.T) {
	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "test-ollama",
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			Capabilities: ProviderCapabilities{Chat: true, Embedding: true},
		},
	})

	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "test-ollama",
		ProviderRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if client.ProviderName() != "ollama" {
		t.Errorf("ProviderName() = %q, want %q", client.ProviderName(), "ollama")
	}
}

func TestNewChatClient_ProviderRegistry_WithAPIKey(t *testing.T) {
	t.Setenv("TEST_KEY_123", "secret-value")

	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "test-zai",
			Type:         "zai",
			Endpoint:     "https://api.z.ai",
			APIKeyEnv:    "TEST_KEY_123",
			ChatPath:     "/api/test/chat",
			Capabilities: ProviderCapabilities{Chat: true},
		},
	})

	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "test-zai",
		ProviderRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if client.ProviderName() != "zai" {
		t.Errorf("ProviderName() = %q, want %q", client.ProviderName(), "zai")
	}
}

func TestNewChatClient_ProviderRegistry_MissingKey(t *testing.T) {
	// The env var MISSING_KEY_XYZ must not exist in the test environment.
	// Do NOT use t.Setenv here — we need it to be truly absent.

	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "test-needs-key",
			Type:         "zai",
			Endpoint:     "https://api.z.ai",
			APIKeyEnv:    "MISSING_KEY_XYZ",
			Capabilities: ProviderCapabilities{Chat: true},
		},
	})

	_, err := NewChatClient(ChatClientConfig{
		ProviderName:     "test-needs-key",
		ProviderRegistry: reg,
	})
	if err == nil {
		t.Error("NewChatClient() expected error for missing API key env var")
	}
}

func TestNewChatClient_ProviderRegistry_UnknownProvider(t *testing.T) {
	reg := NewProviderRegistry([]ProviderConfig{})

	_, err := NewChatClient(ChatClientConfig{
		ProviderName:     "nonexistent",
		ProviderRegistry: reg,
	})
	if err == nil {
		t.Error("NewChatClient() expected error for unknown provider")
	}
}

func TestNewChatClient_ProviderRegistry_NoEndpointRequired(t *testing.T) {
	// When using ProviderRegistry, EndpointURL is not required
	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "test-ollama",
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			Capabilities: ProviderCapabilities{Chat: true},
		},
	})

	// Should NOT error even though EndpointURL is empty
	_, err := NewChatClient(ChatClientConfig{
		ProviderName:     "test-ollama",
		ProviderRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v, want nil (EndpointURL not required with registry)", err)
	}
}

func TestNewChatClient_CapabilityValidation_EmbedOnChatOnly(t *testing.T) {
	// Provider only declares chat, not embedding
	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "chat-only",
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			Capabilities: ProviderCapabilities{Chat: true, Embedding: false},
		},
	})

	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "chat-only",
		ProviderRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}

	// Calling Embed on a chat-only provider should fail at the capability check
	_, err = client.Embed(context.Background(), "some-model", []string{"test"})
	if err == nil {
		t.Error("Embed() expected error: provider does not support embedding")
	}
	if !strings.Contains(err.Error(), "does not support embedding") {
		t.Errorf("Embed() error = %q, want to contain 'does not support embedding'", err.Error())
	}
}

func TestNewChatClient_CapabilityValidation_ChatAllowed(t *testing.T) {
	reg := NewProviderRegistry([]ProviderConfig{
		{
			Name:         "chat-provider",
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			Capabilities: ProviderCapabilities{Chat: true},
		},
	})

	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "chat-provider",
		ProviderRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewChatClient() returned nil")
	}
}

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://api.anthropic.com/v1", "anthropic"},
		{"https://API.ANTHROPIC.COM", "anthropic"},
		{"https://api.z.ai/v1", "zai"},
		{"http://localhost:11434", "ollama"},
		{"http://10.0.0.1:11445", "ollama"},
		{"https://api.openai.com/v1", "openai"},
		{"http://localhost:8000", "openai"},
		{"https://unknown-endpoint.com", "openai"},
	}

	for _, tt := range tests {
		got := detectProvider(tt.url)
		if got != tt.want {
			t.Errorf("detectProvider(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestUnifiedChatClient_ReasoningModeResolver_NoRegistry(t *testing.T) {
	resolver := buildReasoningModeResolver(nil, "zai")
	if got := resolver("any-model"); got != ReasoningModeOff {
		t.Errorf("no-registry resolver = %v; want ReasoningModeOff", got)
	}
}

func TestUnifiedChatClient_ReasoningModeResolver_FromRegistry(t *testing.T) {
	reg := NewEmptyModelRegistry()
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "glm-5.1",
		ProviderModels: map[string]string{"zai": "glm-5.1"},
		ReasoningMode:  ReasoningModeOff,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "non-reasoning-model",
		ProviderModels: map[string]string{"zai": "nrm-v1"},
		ReasoningMode:  ReasoningModeAuto,
	}); err != nil {
		t.Fatal(err)
	}

	resolver := buildReasoningModeResolver(reg, "zai")

	if got := resolver("glm-5.1"); got != ReasoningModeOff {
		t.Errorf("glm-5.1 -> %v; want Off", got)
	}
	if got := resolver("nrm-v1"); got != ReasoningModeAuto {
		t.Errorf("nrm-v1 -> %v; want Auto", got)
	}
	if got := resolver("unknown"); got != ReasoningModeOff {
		t.Errorf("unknown -> %v; want Off (default)", got)
	}
}
