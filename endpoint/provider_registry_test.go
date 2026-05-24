package endpoint

import (
	"testing"
)

func TestNewProviderRegistry(t *testing.T) {
	configs := []ProviderConfig{
		{
			Name:         "zai",
			Type:         "zai",
			Endpoint:     "https://api.z.ai",
			APIKeyEnv:    "ZAI_API_KEY",
			Capabilities: ProviderCapabilities{Chat: true, Thinking: true},
		},
		{
			Name:         "ollama",
			Type:         "ollama",
			Endpoint:     "http://localhost:11434",
			Capabilities: ProviderCapabilities{Chat: true, Embedding: true},
		},
	}

	reg := NewProviderRegistry(configs)
	if reg == nil {
		t.Fatal("NewProviderRegistry returned nil")
	}
}

func TestProviderRegistry_Get(t *testing.T) {
	configs := []ProviderConfig{
		{
			Name:         "zai",
			Type:         "zai",
			Endpoint:     "https://api.z.ai",
			APIKeyEnv:    "ZAI_API_KEY",
			ChatPath:     "/api/coding/paas/v4/chat/completions",
			Capabilities: ProviderCapabilities{Chat: true, Thinking: true},
		},
	}
	reg := NewProviderRegistry(configs)

	t.Run("existing provider", func(t *testing.T) {
		cfg, err := reg.Get("zai")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if cfg.Endpoint != "https://api.z.ai" {
			t.Errorf("Endpoint = %q, want %q", cfg.Endpoint, "https://api.z.ai")
		}
		if cfg.ChatPath != "/api/coding/paas/v4/chat/completions" {
			t.Errorf("ChatPath = %q, want %q", cfg.ChatPath, "/api/coding/paas/v4/chat/completions")
		}
		if !cfg.Capabilities.Chat {
			t.Error("Capabilities.Chat = false, want true")
		}
		if !cfg.Capabilities.Thinking {
			t.Error("Capabilities.Thinking = false, want true")
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, err := reg.Get("unknown")
		if err != ErrProviderNotFound {
			t.Errorf("Get() error = %v, want ErrProviderNotFound", err)
		}
	})
}

func TestProviderRegistry_List(t *testing.T) {
	configs := []ProviderConfig{
		{Name: "a", Type: "ollama", Endpoint: "http://a"},
		{Name: "b", Type: "zai", Endpoint: "http://b"},
	}
	reg := NewProviderRegistry(configs)

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("List() returned %d items, want 2", len(list))
	}
}

func TestProviderCapabilities_HasImageAndVideoFields(t *testing.T) {
	c := ProviderCapabilities{
		Streaming:       true,
		Embedding:       false,
		ImageGeneration: true,
		VideoGeneration: false,
	}
	if !c.ImageGeneration || c.VideoGeneration {
		t.Errorf("flags not as set: %+v", c)
	}
}
