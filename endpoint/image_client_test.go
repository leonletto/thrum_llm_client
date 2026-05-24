package endpoint

import (
	"context"
	"errors"
	"testing"
)

func TestNewImageClient_RequiresEndpointURL(t *testing.T) {
	_, err := NewImageClient(ImageClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing EndpointURL")
	}
}

func TestNewImageClient_AutoDetectsZai(t *testing.T) {
	c, err := NewImageClient(ImageClientConfig{
		EndpointURL: "https://api.z.ai",
		APIKey:      "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProviderName(); got != "zai" {
		t.Errorf("provider = %q; want zai", got)
	}
}

func TestNewImageClient_AutoDetectsOpenRouter(t *testing.T) {
	c, err := NewImageClient(ImageClientConfig{
		EndpointURL: "https://openrouter.ai/api",
		APIKey:      "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProviderName(); got != "openrouter" {
		t.Errorf("provider = %q; want openrouter (auto-detect override)", got)
	}
}

func TestNewImageClient_RejectsUnsupportedProvider(t *testing.T) {
	_, err := NewImageClient(ImageClientConfig{
		EndpointURL: "https://api.anthropic.com",
		Provider:    "anthropic",
		APIKey:      "test",
	})
	if err == nil || !errors.Is(err, ErrCapabilityNotSupported) {
		t.Fatalf("got %v; want ErrCapabilityNotSupported", err)
	}
}

type stubImageAdapter struct{ called bool }

func (s *stubImageAdapter) GenerateImage(_ context.Context, _ ImageOptions) (*ImageResult, error) {
	s.called = true
	return &ImageResult{}, nil
}
func (s *stubImageAdapter) ProviderName() string { return "stub" }

func TestUnifiedImageClient_ProviderName(t *testing.T) {
	uc := &UnifiedImageClient{adapter: &stubImageAdapter{}}
	if got := uc.ProviderName(); got != "stub" {
		t.Errorf("got %q; want stub", got)
	}
}
