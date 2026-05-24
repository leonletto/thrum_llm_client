package endpoint

import (
	"os"
	"testing"
)

func TestLoadProviders_Valid(t *testing.T) {
	providers, err := LoadProviders("testdata/providers.yaml")
	if err != nil {
		t.Fatalf("LoadProviders() error = %v", err)
	}
	if len(providers) != 3 {
		t.Fatalf("LoadProviders() returned %d providers, want 3", len(providers))
	}

	// test-ollama: should inherit defaults + override embedding
	ollama := findProvider(providers, "test-ollama")
	if ollama == nil {
		t.Fatal("test-ollama not found")
	}
	if !ollama.Capabilities.Chat {
		t.Error("test-ollama: Chat should be true (inherited from defaults)")
	}
	if !ollama.Capabilities.Embedding {
		t.Error("test-ollama: Embedding should be true (overridden)")
	}
	if !ollama.Capabilities.Streaming {
		t.Error("test-ollama: Streaming should be true (inherited from defaults)")
	}
	if ollama.Capabilities.Thinking {
		t.Error("test-ollama: Thinking should be false (inherited from defaults)")
	}

	// test-zai: should inherit defaults + override thinking
	zai := findProvider(providers, "test-zai")
	if zai == nil {
		t.Fatal("test-zai not found")
	}
	if !zai.Capabilities.Thinking {
		t.Error("test-zai: Thinking should be true (overridden)")
	}
	if zai.APIKeyEnv != "TEST_ZAI_KEY" {
		t.Errorf("test-zai: APIKeyEnv = %q, want %q", zai.APIKeyEnv, "TEST_ZAI_KEY")
	}
	if zai.ChatPath != "/api/test/chat" {
		t.Errorf("test-zai: ChatPath = %q, want %q", zai.ChatPath, "/api/test/chat")
	}

	// test-nostream: explicitly sets streaming=false, should NOT inherit default true
	nostream := findProvider(providers, "test-nostream")
	if nostream == nil {
		t.Fatal("test-nostream not found")
	}
	if nostream.Capabilities.Streaming {
		t.Error("test-nostream: Streaming should be false (explicitly overridden)")
	}
}

func TestLoadProviders_MissingFile(t *testing.T) {
	_, err := LoadProviders("nonexistent.yaml")
	if err == nil {
		t.Error("LoadProviders() expected error for missing file")
	}
}

func TestLoadProviders_InvalidYAML(t *testing.T) {
	_, err := LoadProviders("testdata/providers_bad.yaml")
	if err == nil {
		t.Error("LoadProviders() expected error for invalid YAML")
	}
}

func TestLoadProviders_NoDefaults(t *testing.T) {
	// Create a temp file with no defaults block
	tmp := t.TempDir() + "/no_defaults.yaml"
	data := []byte("providers:\n  - name: bare\n    type: ollama\n    endpoint: http://localhost:11434\n")
	os.WriteFile(tmp, data, 0644)

	providers, err := LoadProviders(tmp)
	if err != nil {
		t.Fatalf("LoadProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(providers))
	}
	// With no defaults, all bools should be zero-value (false)
	if providers[0].Capabilities.Chat {
		t.Error("bare provider Chat should be false (no defaults)")
	}
}

func findProvider(providers []ProviderConfig, name string) *ProviderConfig {
	for i := range providers {
		if providers[i].Name == name {
			return &providers[i]
		}
	}
	return nil
}

func TestLoadModels_Valid(t *testing.T) {
	profiles, err := LoadModels("testdata/models.yaml")
	if err != nil {
		t.Fatalf("LoadModels() error = %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("LoadModels() returned %d profiles, want 3", len(profiles))
	}

	// test-chat: overrides reasoning, inherits other defaults
	chat := findProfile(profiles, "test-chat")
	if chat == nil {
		t.Fatal("test-chat not found")
	}
	if chat.MaxContextTokens != 128000 {
		t.Errorf("test-chat MaxContextTokens = %d, want 128000", chat.MaxContextTokens)
	}
	if !chat.SupportsReasoning {
		t.Error("test-chat: SupportsReasoning should be true")
	}
	if !chat.SupportsStreaming {
		t.Error("test-chat: SupportsStreaming should be true (inherited)")
	}
	if chat.SupportsEmbedding {
		t.Error("test-chat: SupportsEmbedding should be false (inherited)")
	}
	if len(chat.ProviderModels) != 2 {
		t.Errorf("test-chat: ProviderModels count = %d, want 2", len(chat.ProviderModels))
	}

	// test-embed: overrides streaming=false, embedding=true
	embed := findProfile(profiles, "test-embed")
	if embed == nil {
		t.Fatal("test-embed not found")
	}
	if embed.SupportsStreaming {
		t.Error("test-embed: SupportsStreaming should be false (explicitly set)")
	}
	if !embed.SupportsEmbedding {
		t.Error("test-embed: SupportsEmbedding should be true")
	}
	if embed.MaxContextTokens != 8192 {
		t.Errorf("test-embed MaxContextTokens = %d, want 8192", embed.MaxContextTokens)
	}

	// test-defaults-only: should get all defaults
	defaults := findProfile(profiles, "test-defaults-only")
	if defaults == nil {
		t.Fatal("test-defaults-only not found")
	}
	if defaults.DefaultTemperature != 0.7 {
		t.Errorf("test-defaults-only DefaultTemperature = %f, want 0.7", defaults.DefaultTemperature)
	}
}

func TestLoadModels_MissingFile(t *testing.T) {
	_, err := LoadModels("nonexistent.yaml")
	if err == nil {
		t.Error("LoadModels() expected error for missing file")
	}
}

func TestLoadModels_InvalidYAML(t *testing.T) {
	_, err := LoadModels("testdata/models_bad.yaml")
	if err == nil {
		t.Error("LoadModels() expected error for invalid YAML")
	}
}

func TestLoadModels_InvalidProfile(t *testing.T) {
	// Model with empty canonical_id should fail validation
	tmp := t.TempDir() + "/invalid_model.yaml"
	data := []byte("models:\n  - canonical_id: \"\"\n    display_name: Bad\n    provider_models:\n      ollama: bad\n")
	os.WriteFile(tmp, data, 0644)

	_, err := LoadModels(tmp)
	if err == nil {
		t.Error("LoadModels() expected error for invalid profile (empty canonical_id)")
	}
}

func TestLoadModels_MissingProviderModels(t *testing.T) {
	// Model with no provider_models should fail validation
	tmp := t.TempDir() + "/no_providers.yaml"
	data := []byte("models:\n  - canonical_id: orphan\n    display_name: Orphan\n")
	os.WriteFile(tmp, data, 0644)

	_, err := LoadModels(tmp)
	if err == nil {
		t.Error("LoadModels() expected error for missing provider_models")
	}
}

func findProfile(profiles []*ModelProfile, canonicalID string) *ModelProfile {
	for _, p := range profiles {
		if p.CanonicalID == canonicalID {
			return p
		}
	}
	return nil
}
