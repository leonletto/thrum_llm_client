package endpoint

import (
	"context"
	"errors"
	"testing"
)

func TestNewVideoClient_RequiresEndpointURL(t *testing.T) {
	_, err := NewVideoClient(VideoClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing EndpointURL")
	}
}

func TestNewVideoClient_AutoDetectsZai(t *testing.T) {
	c, err := NewVideoClient(VideoClientConfig{
		EndpointURL: "https://api.z.ai", APIKey: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProviderName(); got != "zai" {
		t.Errorf("provider = %q; want zai", got)
	}
}

func TestNewVideoClient_AutoDetectsOpenAI(t *testing.T) {
	c, err := NewVideoClient(VideoClientConfig{
		EndpointURL: "https://api.openai.com", APIKey: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProviderName(); got != "openai" {
		t.Errorf("provider = %q; want openai", got)
	}
}

func TestNewVideoClient_AutoDetectsOpenRouter(t *testing.T) {
	c, err := NewVideoClient(VideoClientConfig{
		EndpointURL: "https://openrouter.ai/api", APIKey: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProviderName(); got != "openrouter" {
		t.Errorf("provider = %q; want openrouter (auto-detect override)", got)
	}
}

func TestNewVideoClient_RejectsUnsupportedProvider(t *testing.T) {
	_, err := NewVideoClient(VideoClientConfig{
		EndpointURL: "https://api.anthropic.com",
		Provider:    "anthropic", APIKey: "test",
	})
	if err == nil || !errors.Is(err, ErrCapabilityNotSupported) {
		t.Fatalf("got %v; want ErrCapabilityNotSupported", err)
	}
}

type stubVideoAdapter struct{}

func (s *stubVideoAdapter) SubmitVideo(_ context.Context, _ VideoOptions) (*VideoJob, error) {
	return &VideoJob{Status: JobStatusQueued, ID: "stub-1"}, nil
}
func (s *stubVideoAdapter) PollVideo(_ context.Context, _ string) (*VideoJob, error) {
	return &VideoJob{Status: JobStatusCompleted, ID: "stub-1"}, nil
}
func (s *stubVideoAdapter) ProviderName() string { return "stub" }

func TestUnifiedVideoClient_DelegatesSubmit(t *testing.T) {
	uc := &UnifiedVideoClient{adapter: &stubVideoAdapter{}}
	job, err := uc.SubmitVideo(context.Background(), VideoOptions{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "stub-1" || job.Status != JobStatusQueued {
		t.Errorf("got %+v", job)
	}
}

func TestUnifiedVideoClient_WaitDelegatesToPollAsync(t *testing.T) {
	uc := &UnifiedVideoClient{adapter: &stubVideoAdapter{}}
	job, err := uc.WaitVideo(context.Background(), "stub-1", PollOptions{Interval: 1, MaxWait: 1000000000})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusCompleted {
		t.Errorf("got status %v; want Completed", job.Status)
	}
}
