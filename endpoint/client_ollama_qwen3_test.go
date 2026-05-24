//go:build integration

package endpoint

import (
	"context"
	"testing"
	"time"
)

func TestOllamaQwen3_Integration(t *testing.T) {
	providers, err := LoadProviders("../../../config/providers.yaml")
	if err != nil {
		t.Fatalf("providers: %v", err)
	}
	modelReg := NewEmptyModelRegistry()
	if err := modelReg.LoadFromFile("../../../config/models.yaml"); err != nil {
		t.Fatalf("models: %v", err)
	}
	client, err := NewChatClient(ChatClientConfig{
		ProviderName:     "ollama",
		ProviderRegistry: NewProviderRegistry(providers),
		ModelRegistry:    modelReg,
	})
	if err != nil {
		t.Skipf("ollama not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Chat(ctx, "qwen3-30b-instruct", []ChatMessage{
		{Role: "user", Content: "Reply with exactly: HELLO"},
	}, &ChatOptions{MaxTokens: 64, Temperature: 0.0})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content == "" {
		t.Errorf("empty response")
	}
	t.Logf("qwen3 replied: %q", resp.Content)
}
