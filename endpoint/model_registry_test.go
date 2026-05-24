package endpoint

import (
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNewEmptyModelRegistry(t *testing.T) {
	r := NewEmptyModelRegistry()

	profiles := r.ListProfiles()
	if len(profiles) != 0 {
		t.Errorf("NewEmptyModelRegistry should have no profiles, got %d", len(profiles))
	}
}

func TestModelProfile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		profile *ModelProfile
		wantErr bool
	}{
		{
			name: "valid profile",
			profile: &ModelProfile{
				CanonicalID:    "test-model",
				ProviderModels: map[string]string{"test": "test-model-v1"},
			},
			wantErr: false,
		},
		{
			name: "empty canonical ID",
			profile: &ModelProfile{
				CanonicalID:    "",
				ProviderModels: map[string]string{"test": "test-model-v1"},
			},
			wantErr: true,
		},
		{
			name: "nil provider models",
			profile: &ModelProfile{
				CanonicalID:    "test-model",
				ProviderModels: nil,
			},
			wantErr: true,
		},
		{
			name: "empty provider models",
			profile: &ModelProfile{
				CanonicalID:    "test-model",
				ProviderModels: map[string]string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.profile.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModelRegistry_Register(t *testing.T) {
	r := NewEmptyModelRegistry()

	profile := &ModelProfile{
		CanonicalID:      "test-model",
		DisplayName:      "Test Model",
		MaxContextTokens: 4096,
		ProviderModels:   map[string]string{"test": "test-model-v1"},
	}

	// First registration should succeed
	err := r.Register(profile)
	if err != nil {
		t.Errorf("Register() error = %v, want nil", err)
	}

	// Duplicate registration should fail
	err = r.Register(profile)
	if err != ErrDuplicateModelProfile {
		t.Errorf("Register() error = %v, want ErrDuplicateModelProfile", err)
	}

	// Invalid profile should fail
	err = r.Register(&ModelProfile{CanonicalID: ""})
	if err != ErrInvalidModelProfile {
		t.Errorf("Register() error = %v, want ErrInvalidModelProfile", err)
	}
}

func TestModelRegistry_ResolveModel(t *testing.T) {
	r := NewEmptyModelRegistry()

	profile := &ModelProfile{
		CanonicalID: "qwen3",
		ProviderModels: map[string]string{
			"ollama": "qwen3:7b",
			"vllm":   "Qwen/Qwen3-7B",
		},
	}
	_ = r.Register(profile)

	tests := []struct {
		name        string
		canonicalID string
		provider    string
		want        string
		wantErr     error
	}{
		{
			name:        "resolve ollama",
			canonicalID: "qwen3",
			provider:    "ollama",
			want:        "qwen3:7b",
			wantErr:     nil,
		},
		{
			name:        "resolve vllm",
			canonicalID: "qwen3",
			provider:    "vllm",
			want:        "Qwen/Qwen3-7B",
			wantErr:     nil,
		},
		{
			name:        "unknown provider",
			canonicalID: "qwen3",
			provider:    "openai",
			want:        "",
			wantErr:     ErrProviderNotSupported,
		},
		{
			name:        "unknown model",
			canonicalID: "unknown-model",
			provider:    "ollama",
			want:        "",
			wantErr:     ErrModelProfileNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.ResolveModel(tt.canonicalID, tt.provider)
			if err != tt.wantErr {
				t.Errorf("ResolveModel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModelRegistry_GetProfile(t *testing.T) {
	r := NewEmptyModelRegistry()
	if err := r.LoadFromFile("testdata/models.yaml"); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}

	profile, err := r.GetProfile("test-chat")
	if err != nil {
		t.Errorf("GetProfile() error = %v, want nil", err)
	}
	if profile.CanonicalID != "test-chat" {
		t.Errorf("GetProfile().CanonicalID = %q, want %q", profile.CanonicalID, "test-chat")
	}
	if profile.MaxContextTokens != 128000 {
		t.Errorf("GetProfile().MaxContextTokens = %d, want %d", profile.MaxContextTokens, 128000)
	}

	_, err = r.GetProfile("non-existent")
	if err != ErrModelProfileNotFound {
		t.Errorf("GetProfile() error = %v, want ErrModelProfileNotFound", err)
	}
}

func TestModelRegistry_AutoDetect(t *testing.T) {
	r := NewEmptyModelRegistry()
	if err := r.LoadFromFile("testdata/models.yaml"); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}

	tests := []struct {
		name          string
		providerModel string
		wantCanonical string
		wantErr       error
	}{
		{
			name:          "detect ollama model",
			providerModel: "test-chat:7b",
			wantCanonical: "test-chat",
		},
		{
			name:          "detect vllm model",
			providerModel: "Test/Chat-7B",
			wantCanonical: "test-chat",
		},
		{
			name:          "detect case-insensitive",
			providerModel: "TEST-CHAT:7B",
			wantCanonical: "test-chat",
		},
		{
			name:          "detect embed model",
			providerModel: "test-embed:latest",
			wantCanonical: "test-embed",
		},
		{
			name:          "unknown model",
			providerModel: "unknown-model",
			wantErr:       ErrModelProfileNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := r.AutoDetect(tt.providerModel)
			if err != tt.wantErr {
				t.Errorf("AutoDetect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && profile.CanonicalID != tt.wantCanonical {
				t.Errorf("AutoDetect().CanonicalID = %v, want %v", profile.CanonicalID, tt.wantCanonical)
			}
		})
	}
}

func TestModelRegistry_HasProvider(t *testing.T) {
	r := NewEmptyModelRegistry()
	if err := r.LoadFromFile("testdata/models.yaml"); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}

	if !r.HasProvider("test-chat", "ollama") {
		t.Error("HasProvider(test-chat, ollama) = false, want true")
	}
	if !r.HasProvider("test-chat", "vllm") {
		t.Error("HasProvider(test-chat, vllm) = false, want true")
	}
	if r.HasProvider("test-chat", "openai") {
		t.Error("HasProvider(test-chat, openai) = true, want false")
	}

	if r.HasProvider("unknown-model", "ollama") {
		t.Error("HasProvider(unknown-model, ollama) = true, want false")
	}
}

func TestModelRegistry_ListProfiles(t *testing.T) {
	r := NewEmptyModelRegistry()
	if err := r.LoadFromFile("testdata/models.yaml"); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}

	profiles := r.ListProfiles()
	if len(profiles) != 3 {
		t.Errorf("ListProfiles() returned %d profiles, want 3", len(profiles))
	}

	for _, p := range profiles {
		if p.CanonicalID == "" {
			t.Error("ListProfiles() returned profile with empty CanonicalID")
		}
	}
}

func TestModelRegistry_ConcurrentAccess(t *testing.T) {
	r := NewEmptyModelRegistry()
	if err := r.LoadFromFile("testdata/models.yaml"); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}

	var wg sync.WaitGroup
	goroutines := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.GetProfile("test-chat")
			_, _ = r.ResolveModel("test-chat", "ollama")
			_, _ = r.AutoDetect("test-chat:7b")
			_ = r.ListProfiles()
		}()
	}

	wg.Wait()
}

func TestModelRegistry_LoadFromFile(t *testing.T) {
	r := NewEmptyModelRegistry()

	err := r.LoadFromFile("testdata/models.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	profiles := r.ListProfiles()
	if len(profiles) != 3 {
		t.Errorf("LoadFromFile() loaded %d profiles, want 3", len(profiles))
	}

	p, err := r.GetProfile("test-chat")
	if err != nil {
		t.Fatalf("GetProfile(test-chat) error = %v", err)
	}
	if p.DisplayName != "Test Chat Model" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "Test Chat Model")
	}

	detected, err := r.AutoDetect("test-chat:7b")
	if err != nil {
		t.Fatalf("AutoDetect() error = %v", err)
	}
	if detected.CanonicalID != "test-chat" {
		t.Errorf("AutoDetect().CanonicalID = %q, want %q", detected.CanonicalID, "test-chat")
	}
}

func TestModelRegistry_LoadFromFile_Overwrite(t *testing.T) {
	r := NewEmptyModelRegistry()

	_ = r.Register(&ModelProfile{
		CanonicalID:    "test-chat",
		DisplayName:    "Old Name",
		ProviderModels: map[string]string{"old": "old-model"},
	})

	err := r.LoadFromFile("testdata/models.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	p, _ := r.GetProfile("test-chat")
	if p.DisplayName != "Test Chat Model" {
		t.Errorf("DisplayName = %q, want %q (should be overwritten)", p.DisplayName, "Test Chat Model")
	}
}

func TestModelRegistry_LoadFromFile_MissingFile(t *testing.T) {
	r := NewEmptyModelRegistry()
	err := r.LoadFromFile("nonexistent.yaml")
	if err == nil {
		t.Error("LoadFromFile() expected error for missing file")
	}
}

func TestModelProfile_ReasoningModeDefault(t *testing.T) {
	p := &ModelProfile{
		CanonicalID:    "test",
		ProviderModels: map[string]string{"zai": "test-zai"},
	}
	if p.ReasoningMode != ReasoningModeOff {
		t.Fatalf("default ReasoningMode = %v; want ReasoningModeOff", p.ReasoningMode)
	}
}

func TestModelProfile_YAMLRoundTrip_ReasoningMode(t *testing.T) {
	cases := []struct {
		yaml string
		want ReasoningMode
	}{
		{"canonical_id: a\nprovider_models: {zai: a}\nreasoning_mode: off\n", ReasoningModeOff},
		{"canonical_id: a\nprovider_models: {zai: a}\nreasoning_mode: auto\n", ReasoningModeAuto},
		{"canonical_id: a\nprovider_models: {zai: a}\nreasoning_mode: on\n", ReasoningModeOn},
		{"canonical_id: a\nprovider_models: {zai: a}\n", ReasoningModeOff},
	}
	for _, tc := range cases {
		var p ModelProfile
		if err := yaml.Unmarshal([]byte(tc.yaml), &p); err != nil {
			t.Fatalf("unmarshal %q: %v", tc.yaml, err)
		}
		if p.ReasoningMode != tc.want {
			t.Errorf("yaml=%q got=%v want=%v", tc.yaml, p.ReasoningMode, tc.want)
		}
	}
}

func TestModelRegistry_ResolveReasoningMode_CrossProviderCollision(t *testing.T) {
	// When two profiles use the same provider-specific model name
	// (string) under different providers, the reverse index keeps only
	// the most recently registered binding. The resolver must not leak
	// the surviving profile's mode to a provider that has no binding
	// for that model — the cross-provider membership check must
	// return ReasoningModeOff in that case.
	reg := NewEmptyModelRegistry()
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "early",
		ProviderModels: map[string]string{"zai": "shared"},
		ReasoningMode:  ReasoningModeAuto,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "late",
		ProviderModels: map[string]string{"openai": "shared"},
		ReasoningMode:  ReasoningModeOn,
	}); err != nil {
		t.Fatal(err)
	}

	// openai owns "shared" in the reverse index; resolve returns its mode.
	if got := reg.ResolveReasoningMode("shared", "openai"); got != ReasoningModeOn {
		t.Errorf("openai/shared = %v; want On", got)
	}
	// zai's binding was overwritten in the reverse index. The cross-
	// provider guard MUST keep us from leaking the openai profile's
	// On mode back to zai.
	if got := reg.ResolveReasoningMode("shared", "zai"); got != ReasoningModeOff {
		t.Errorf("zai/shared after overwrite = %v; want Off (collision guard)", got)
	}
	// Unknown model → Off.
	if got := reg.ResolveReasoningMode("nope", "zai"); got != ReasoningModeOff {
		t.Errorf("unknown/zai = %v; want Off", got)
	}
}

func TestModelRegistry_ResolveReasoningMode_CaseInsensitive(t *testing.T) {
	reg := NewEmptyModelRegistry()
	if err := reg.Register(&ModelProfile{
		CanonicalID:    "glm",
		ProviderModels: map[string]string{"zai": "GLM-5.1"},
		ReasoningMode:  ReasoningModeOff,
	}); err != nil {
		t.Fatal(err)
	}
	if got := reg.ResolveReasoningMode("glm-5.1", "zai"); got != ReasoningModeOff {
		t.Errorf("lowercase lookup = %v; want Off (registered)", got)
	}
}
