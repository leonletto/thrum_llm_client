package endpoint

import (
	"errors"
	"strings"
	"sync"
)

// Model registry errors.
var (
	// ErrModelProfileNotFound is returned when a model profile is not registered.
	ErrModelProfileNotFound = errors.New("model profile not found")

	// ErrProviderNotSupported is returned when a provider mapping is not defined for a model.
	ErrProviderNotSupported = errors.New("provider not supported for this model")

	// ErrDuplicateModelProfile is returned when attempting to register a duplicate profile.
	ErrDuplicateModelProfile = errors.New("model profile already registered")

	// ErrInvalidModelProfile is returned when a model profile has invalid configuration.
	ErrInvalidModelProfile = errors.New("invalid model profile")
)

// ModelProfile defines a canonical model with provider-specific mappings.
type ModelProfile struct {
	// CanonicalID is the unified model identifier (e.g., "qwen3-30b-instruct")
	CanonicalID string `json:"canonical_id" yaml:"canonical_id"`

	// DisplayName is the human-readable model name
	DisplayName string `json:"display_name" yaml:"display_name"`

	// MaxContextTokens is the maximum context window size
	MaxContextTokens int `json:"max_context_tokens" yaml:"max_context_tokens"`

	// MaxOutputTokens is the maximum output token limit
	MaxOutputTokens int `json:"max_output_tokens" yaml:"max_output_tokens"`

	// SupportsStreaming indicates if the model supports streaming responses
	SupportsStreaming bool `json:"supports_streaming" yaml:"supports_streaming"`

	// SupportsEmbedding indicates if the model supports embedding generation
	SupportsEmbedding bool `json:"supports_embedding" yaml:"supports_embedding"`

	// SupportsReasoning indicates if the model supports extended thinking/reasoning
	SupportsReasoning bool `json:"supports_reasoning" yaml:"supports_reasoning"`

	// SupportsVision indicates if the model supports vision/image input
	SupportsVision bool `json:"supports_vision" yaml:"supports_vision"`

	// ProviderModels maps provider names to provider-specific model identifiers
	// e.g., {"ollama": "qwen3:30b-a3b-instruct", "vllm": "Qwen/Qwen3-30B-A3B-Instruct"}
	ProviderModels map[string]string `json:"provider_models" yaml:"provider_models"`

	// DefaultTemperature is the recommended temperature for this model
	DefaultTemperature float64 `json:"default_temperature" yaml:"default_temperature"`

	// ReasoningMode controls the wire-shape of the reasoning-control
	// field for providers that support it (currently Z.ai's "thinking"
	// block). Zero value (ReasoningModeOff) emits the field explicitly
	// — the safe default for reasoning models. See ReasoningMode docs
	// for off/auto/on semantics.
	ReasoningMode ReasoningMode `json:"reasoning_mode,omitempty" yaml:"reasoning_mode,omitempty"`
}

// Validate checks if the model profile has valid configuration.
func (p *ModelProfile) Validate() error {
	if p.CanonicalID == "" {
		return ErrInvalidModelProfile
	}
	if len(p.ProviderModels) == 0 {
		return ErrInvalidModelProfile
	}
	return nil
}

// ModelRegistry manages model profiles and provides translation between
// canonical model names and provider-specific identifiers.
type ModelRegistry struct {
	mu       sync.RWMutex
	profiles map[string]*ModelProfile

	// reverseIndex maps provider model names to canonical IDs for AutoDetect
	reverseIndex map[string]string
}

// NewEmptyModelRegistry creates a new ModelRegistry without pre-registered models.
func NewEmptyModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		profiles:     make(map[string]*ModelProfile),
		reverseIndex: make(map[string]string),
	}
}

// Register adds a new model profile to the registry.
func (r *ModelRegistry) Register(profile *ModelProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.profiles[profile.CanonicalID]; exists {
		return ErrDuplicateModelProfile
	}

	r.profiles[profile.CanonicalID] = profile

	// Build reverse index for AutoDetect
	for _, providerModel := range profile.ProviderModels {
		normalizedModel := strings.ToLower(providerModel)
		r.reverseIndex[normalizedModel] = profile.CanonicalID
	}

	return nil
}

// ResolveModel translates a canonical model ID to a provider-specific model name.
func (r *ModelRegistry) ResolveModel(canonicalID, provider string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profile, exists := r.profiles[canonicalID]
	if !exists {
		return "", ErrModelProfileNotFound
	}

	providerModel, exists := profile.ProviderModels[provider]
	if !exists {
		return "", ErrProviderNotSupported
	}

	return providerModel, nil
}

// GetProfile retrieves a model profile by canonical ID.
func (r *ModelRegistry) GetProfile(canonicalID string) (*ModelProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profile, exists := r.profiles[canonicalID]
	if !exists {
		return nil, ErrModelProfileNotFound
	}

	return profile, nil
}

// AutoDetect attempts to find a model profile from a provider-specific model name.
func (r *ModelRegistry) AutoDetect(providerModel string) (*ModelProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	normalizedModel := strings.ToLower(providerModel)
	canonicalID, exists := r.reverseIndex[normalizedModel]
	if !exists {
		return nil, ErrModelProfileNotFound
	}

	return r.profiles[canonicalID], nil
}

// ResolveReasoningMode returns the ReasoningMode for the given
// provider-specific model name when that model is registered for the
// given provider. Returns ReasoningModeOff when the model is not
// registered, or is registered only for other providers (cross-
// provider reverse-index collision). The lookup, the cross-provider
// membership check, and the mode read all happen under a single
// RLock acquisition to avoid a data race against concurrent
// Register / LoadFromDB / LoadFromFile mutating the profile map.
func (r *ModelRegistry) ResolveReasoningMode(providerModel, provider string) ReasoningMode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	canonicalID, ok := r.reverseIndex[strings.ToLower(providerModel)]
	if !ok {
		return ReasoningModeOff
	}
	profile, ok := r.profiles[canonicalID]
	if !ok {
		return ReasoningModeOff
	}
	if _, ok := profile.ProviderModels[provider]; !ok {
		return ReasoningModeOff
	}
	return profile.ReasoningMode
}

// ListProfiles returns all registered model profiles.
func (r *ModelRegistry) ListProfiles() []*ModelProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles := make([]*ModelProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		profiles = append(profiles, p)
	}
	return profiles
}

// HasProvider checks if the canonical model supports a specific provider.
func (r *ModelRegistry) HasProvider(canonicalID, provider string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profile, exists := r.profiles[canonicalID]
	if !exists {
		return false
	}

	_, hasProvider := profile.ProviderModels[provider]
	return hasProvider
}

// ModelProfileData carries model profile data from an external source (DB).
// Used by LoadFromDB to register profiles without importing localstore.
type ModelProfileData struct {
	CanonicalID       string
	DisplayName       string
	ProviderModels    map[string]string
	MaxContext        int
	MaxOutput         int
	Temperature       float64
	SupportsStreaming bool
	SupportsReasoning bool
	SupportsEmbedding bool
	SupportsVision    bool
	ReasoningMode     ReasoningMode
}

// LoadFromDB registers model profiles from external data, replacing any
// existing profiles with the same CanonicalID. Call after LoadFromFile
// to override file-sourced entries with DB-sourced ones.
func (r *ModelRegistry) LoadFromDB(profiles []ModelProfileData) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	loaded := 0
	for _, p := range profiles {
		r.registerNoLock(&ModelProfile{
			CanonicalID:        p.CanonicalID,
			DisplayName:        p.DisplayName,
			MaxContextTokens:   p.MaxContext,
			MaxOutputTokens:    p.MaxOutput,
			SupportsStreaming:  p.SupportsStreaming,
			SupportsReasoning:  p.SupportsReasoning,
			SupportsEmbedding:  p.SupportsEmbedding,
			SupportsVision:     p.SupportsVision,
			ProviderModels:     p.ProviderModels,
			DefaultTemperature: p.Temperature,
			ReasoningMode:      p.ReasoningMode,
		})
		loaded++
	}
	return loaded
}

// LoadFromFile reads model profiles from a YAML file and registers them
// using overwrite semantics. Each profile is validated before insertion.
func (r *ModelRegistry) LoadFromFile(path string) error {
	profiles, err := LoadModels(path)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range profiles {
		r.registerNoLock(p)
	}

	return nil
}

// registerNoLock registers a profile without acquiring the lock.
// Used internally during initialization.
func (r *ModelRegistry) registerNoLock(profile *ModelProfile) {
	// Clean up stale reverse index entries from previous profile
	if old, exists := r.profiles[profile.CanonicalID]; exists {
		for _, providerModel := range old.ProviderModels {
			delete(r.reverseIndex, strings.ToLower(providerModel))
		}
	}

	r.profiles[profile.CanonicalID] = profile

	for _, providerModel := range profile.ProviderModels {
		normalizedModel := strings.ToLower(providerModel)
		r.reverseIndex[normalizedModel] = profile.CanonicalID
	}
}
