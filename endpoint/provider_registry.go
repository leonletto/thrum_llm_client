package endpoint

// ProviderCapabilities declares what operations a provider supports.
type ProviderCapabilities struct {
	Chat      bool `yaml:"chat"`
	Embedding bool `yaml:"embedding"`
	Vision    bool `yaml:"vision"`
	Thinking  bool `yaml:"thinking"`
	Streaming bool `yaml:"streaming"`
	Generate  bool `yaml:"generate"`
	// ImageGeneration indicates the provider supports the
	// endpoint.ImageClient surface (POST image generations).
	ImageGeneration bool `yaml:"image_generation,omitempty"`
	// VideoGeneration indicates the provider supports the
	// endpoint.VideoClient surface (async video generation).
	VideoGeneration bool `yaml:"video_generation,omitempty"`
}

// ProviderConfig describes a configured LLM provider endpoint.
type ProviderConfig struct {
	Name         string               `yaml:"name"`
	Type         string               `yaml:"type"`
	Endpoint     string               `yaml:"endpoint"`
	APIKeyEnv    string               `yaml:"api_key_env"`
	ChatPath     string               `yaml:"chat_path"`
	ExtraHeaders map[string]string    `yaml:"extra_headers"`
	Capabilities ProviderCapabilities `yaml:"capabilities"`
}

// ProviderRegistry holds provider configurations indexed by name.
type ProviderRegistry struct {
	providers map[string]ProviderConfig
}

// NewProviderRegistry creates a ProviderRegistry from a slice of configs.
func NewProviderRegistry(configs []ProviderConfig) *ProviderRegistry {
	r := &ProviderRegistry{
		providers: make(map[string]ProviderConfig, len(configs)),
	}
	for _, c := range configs {
		r.providers[c.Name] = c
	}
	return r
}

// Get returns the ProviderConfig for the given name.
func (r *ProviderRegistry) Get(name string) (ProviderConfig, error) {
	cfg, ok := r.providers[name]
	if !ok {
		return ProviderConfig{}, ErrProviderNotFound
	}
	return cfg, nil
}

// List returns all provider configurations.
func (r *ProviderRegistry) List() []ProviderConfig {
	list := make([]ProviderConfig, 0, len(r.providers))
	for _, c := range r.providers {
		list = append(list, c)
	}
	return list
}
