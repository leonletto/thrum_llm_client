//go:build e2e

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/leonletto/thrum_llm_client/endpoint"
)

const openrouterChatModel = "qwen/qwen3.5-35b-a3b"

// base only — provider paths (/v1/chat/completions, /v1/videos) supply
// the version segment. Do NOT append /v1 here or paths will double up.
const openrouterEndpoint = "https://openrouter.ai/api"

func TestOpenRouterChat(t *testing.T) {
	apiKey := requireEnv(t, "OPENROUTER_API_KEY")

	newClient := func(t *testing.T) *endpoint.UnifiedChatClient {
		t.Helper()
		c, err := endpoint.NewChatClient(endpoint.ChatClientConfig{
			EndpointURL: openrouterEndpoint,
			APIKey:      apiKey,
			// Qwen3.5 is a reasoning-default model on OR; with reasoning
			// enabled it consumed all 256 MaxTokens on internal reasoning
			// and returned empty content. Disable so the visible content
			// stays within MaxTokens.
			ExtraBody: map[string]any{
				"reasoning": map[string]any{"enabled": false},
			},
		})
		if err != nil {
			t.Fatalf("NewChatClient: %v", err)
		}
		return c
	}

	msgs := []endpoint.ChatMessage{
		{Role: "user", Content: "Say hello in one short sentence."},
	}
	// 256 because reasoning-default models (glm-5.1, qwen3.5) burn ~60
	// tokens on reasoning before producing visible content; 64 is too
	// tight and yields finish_reason=length with empty Content.
	opts := &endpoint.ChatOptions{MaxTokens: 256}

	t.Run("NonStreaming", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		resp, err := newClient(t).Chat(ctx, openrouterChatModel, msgs, opts)
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if strings.TrimSpace(resp.Content) == "" {
			t.Fatalf("Chat returned empty Content")
		}
		if resp.TokensUsed.PromptTokens <= 0 {
			t.Fatalf("Chat returned PromptTokens=%d; want > 0", resp.TokensUsed.PromptTokens)
		}
		if resp.TokensUsed.CompletionTokens <= 0 {
			t.Fatalf("Chat returned CompletionTokens=%d; want > 0", resp.TokensUsed.CompletionTokens)
		}
	})

	t.Run("Streaming", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var sb strings.Builder
		callbackCount := 0
		_, err := newClient(t).ChatStream(ctx, openrouterChatModel, msgs, opts,
			func(chunk string) error {
				sb.WriteString(chunk)
				callbackCount++
				return nil
			})
		if err != nil {
			t.Fatalf("ChatStream: %v", err)
		}
		if strings.TrimSpace(sb.String()) == "" {
			t.Fatalf("ChatStream accumulated empty content")
		}
		if callbackCount == 0 {
			t.Fatalf("ChatStream callback fired 0 times; want > 0")
		}
	})
}
