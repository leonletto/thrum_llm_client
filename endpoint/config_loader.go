package endpoint

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// --- Provider config loading ---

// rawProviderCapabilities uses *bool pointers to distinguish
// "explicitly set to false" from "omitted" (nil = inherit default).
type rawProviderCapabilities struct {
	Chat      *bool `yaml:"chat"`
	Embedding *bool `yaml:"embedding"`
	Vision    *bool `yaml:"vision"`
	Thinking  *bool `yaml:"thinking"`
	Streaming *bool `yaml:"streaming"`
	Generate  *bool `yaml:"generate"`
}

type rawProviderConfig struct {
	Name         string                  `yaml:"name"`
	Type         string                  `yaml:"type"`
	Endpoint     string                  `yaml:"endpoint"`
	APIKeyEnv    string                  `yaml:"api_key_env"`
	ChatPath     string                  `yaml:"chat_path"`
	ExtraHeaders map[string]string       `yaml:"extra_headers"`
	Capabilities rawProviderCapabilities `yaml:"capabilities"`
}

type rawProviderFile struct {
	Defaults struct {
		Capabilities rawProviderCapabilities `yaml:"capabilities"`
	} `yaml:"defaults"`
	Providers []rawProviderConfig `yaml:"providers"`
}

// LoadProviders reads a providers YAML file and returns merged ProviderConfig entries.
func LoadProviders(path string) ([]ProviderConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load providers: %w", err)
	}

	var raw rawProviderFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("load providers: %w", err)
	}

	result := make([]ProviderConfig, len(raw.Providers))
	for i, rp := range raw.Providers {
		result[i] = ProviderConfig{
			Name:         rp.Name,
			Type:         rp.Type,
			Endpoint:     rp.Endpoint,
			APIKeyEnv:    rp.APIKeyEnv,
			ChatPath:     rp.ChatPath,
			ExtraHeaders: rp.ExtraHeaders,
			Capabilities: mergeProviderCapabilities(raw.Defaults.Capabilities, rp.Capabilities),
		}
	}

	return result, nil
}

// mergeProviderCapabilities merges entry capabilities over defaults.
// If the entry pointer is non-nil, use it; otherwise use the default.
func mergeProviderCapabilities(defaults, entry rawProviderCapabilities) ProviderCapabilities {
	return ProviderCapabilities{
		Chat:      mergeBool(defaults.Chat, entry.Chat),
		Embedding: mergeBool(defaults.Embedding, entry.Embedding),
		Vision:    mergeBool(defaults.Vision, entry.Vision),
		Thinking:  mergeBool(defaults.Thinking, entry.Thinking),
		Streaming: mergeBool(defaults.Streaming, entry.Streaming),
		Generate:  mergeBool(defaults.Generate, entry.Generate),
	}
}

// mergeBool returns the entry value if non-nil, else the default value if non-nil, else false.
func mergeBool(defaultVal, entryVal *bool) bool {
	if entryVal != nil {
		return *entryVal
	}
	if defaultVal != nil {
		return *defaultVal
	}
	return false
}

// --- Model config loading ---

// rawModelProfile uses *bool pointers for boolean fields and *int/*float64
// for numeric fields to distinguish "explicitly set" from "omitted".
type rawModelProfile struct {
	CanonicalID        string            `yaml:"canonical_id"`
	DisplayName        string            `yaml:"display_name"`
	MaxContextTokens   *int              `yaml:"max_context_tokens"`
	MaxOutputTokens    *int              `yaml:"max_output_tokens"`
	DefaultTemperature *float64          `yaml:"default_temperature"`
	SupportsStreaming  *bool             `yaml:"supports_streaming"`
	SupportsEmbedding  *bool             `yaml:"supports_embedding"`
	SupportsReasoning  *bool             `yaml:"supports_reasoning"`
	SupportsVision     *bool             `yaml:"supports_vision"`
	ProviderModels     map[string]string `yaml:"provider_models"`
}

type rawModelFile struct {
	Defaults rawModelProfile   `yaml:"defaults"`
	Models   []rawModelProfile `yaml:"models"`
}

// LoadModels reads a models YAML file, merges defaults, validates each profile,
// and returns the result. Returns an error if any profile fails validation.
func LoadModels(path string) ([]*ModelProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load models: %w", err)
	}

	var raw rawModelFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("load models: %w", err)
	}

	profiles := make([]*ModelProfile, 0, len(raw.Models))
	for _, rm := range raw.Models {
		p := &ModelProfile{
			CanonicalID:        rm.CanonicalID,
			DisplayName:        rm.DisplayName,
			MaxContextTokens:   mergeInt(raw.Defaults.MaxContextTokens, rm.MaxContextTokens),
			MaxOutputTokens:    mergeInt(raw.Defaults.MaxOutputTokens, rm.MaxOutputTokens),
			DefaultTemperature: mergeFloat(raw.Defaults.DefaultTemperature, rm.DefaultTemperature),
			SupportsStreaming:  mergeBool(raw.Defaults.SupportsStreaming, rm.SupportsStreaming),
			SupportsEmbedding:  mergeBool(raw.Defaults.SupportsEmbedding, rm.SupportsEmbedding),
			SupportsReasoning:  mergeBool(raw.Defaults.SupportsReasoning, rm.SupportsReasoning),
			SupportsVision:     mergeBool(raw.Defaults.SupportsVision, rm.SupportsVision),
			ProviderModels:     rm.ProviderModels,
		}

		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("load models: profile %q: %w", rm.CanonicalID, err)
		}

		profiles = append(profiles, p)
	}

	return profiles, nil
}

// mergeInt returns the entry value if non-nil, else default, else 0.
func mergeInt(defaultVal, entryVal *int) int {
	if entryVal != nil {
		return *entryVal
	}
	if defaultVal != nil {
		return *defaultVal
	}
	return 0
}

// mergeFloat returns the entry value if non-nil, else default, else 0.
func mergeFloat(defaultVal, entryVal *float64) float64 {
	if entryVal != nil {
		return *entryVal
	}
	if defaultVal != nil {
		return *defaultVal
	}
	return 0
}
