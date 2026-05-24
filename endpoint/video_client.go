package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// VideoClient is the public-facing video-generation surface.
type VideoClient interface {
	// SubmitVideo enqueues an async generation job and returns the
	// initial job state. The job will typically be in JobStatusQueued
	// or JobStatusInProgress at this point.
	SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error)

	// PollVideo fetches the latest state of an existing job by ID.
	PollVideo(ctx context.Context, jobID string) (*VideoJob, error)

	// WaitVideo blocks until the job reaches a terminal state, the
	// context cancels, or PollOptions.MaxWait elapses (returns
	// ErrPollTimeout in the latter case). Implementation calls
	// PollVideo in a loop via the shared pollAsync helper.
	WaitVideo(ctx context.Context, jobID string, opts PollOptions) (*VideoJob, error)

	ProviderName() string
}

// VideoClientAdapter is implemented by per-provider video adapters.
// Note: WaitVideo is NOT on this interface — UnifiedVideoClient
// implements Wait by calling adapter.PollVideo via pollAsync, so
// adapters only implement Submit and Poll.
type VideoClientAdapter interface {
	SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error)
	PollVideo(ctx context.Context, jobID string) (*VideoJob, error)
	ProviderName() string
}

// VideoClientConfig configures the UnifiedVideoClient.
type VideoClientConfig struct {
	EndpointURL  string
	Provider     string
	APIKey       string
	HTTPClient   *http.Client
	ExtraHeaders map[string]string
	ExtraBody    map[string]any
}

// UnifiedVideoClient wraps a per-provider video adapter.
type UnifiedVideoClient struct {
	adapter VideoClientAdapter
}

// NewVideoClient constructs a video client with provider auto-detection.
func NewVideoClient(cfg VideoClientConfig) (*UnifiedVideoClient, error) {
	if cfg.EndpointURL == "" {
		return nil, fmt.Errorf("EndpointURL is required")
	}
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg.EndpointURL)
		if provider == "openai" && strings.Contains(strings.ToLower(cfg.EndpointURL), "openrouter.ai") {
			provider = "openrouter"
		}
	}

	adapter, err := createVideoAdapter(provider, cfg.EndpointURL, cfg.APIKey, cfg.ExtraHeaders)
	if err != nil {
		return nil, err
	}
	if cc, ok := adapter.(extraBodyConfigurable); ok {
		if cfg.HTTPClient != nil {
			cc.setHTTPClient(cfg.HTTPClient)
		}
		if len(cfg.ExtraBody) > 0 {
			cc.setExtraBody(cfg.ExtraBody)
		}
		if len(cfg.ExtraHeaders) > 0 {
			cc.setExtraHeaders(cfg.ExtraHeaders)
		}
	}
	return &UnifiedVideoClient{adapter: adapter}, nil
}

func createVideoAdapter(provider, url, apiKey string, extraHeaders map[string]string) (VideoClientAdapter, error) {
	switch provider {
	case "zai":
		return newVideoZaiClient(url, apiKey, extraHeaders), nil
	case "openai", "openai-compat":
		return newVideoOpenAIClient(url, apiKey, extraHeaders), nil
	case "openrouter":
		return newVideoOpenRouterClient(url, apiKey, extraHeaders), nil
	case "anthropic", "ollama":
		return nil, fmt.Errorf("provider %q video generation: %w", provider, ErrCapabilityNotSupported)
	default:
		return nil, fmt.Errorf("provider %q video generation: %w", provider, ErrCapabilityNotSupported)
	}
}

// SubmitVideo delegates to the adapter.
func (c *UnifiedVideoClient) SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error) {
	return c.adapter.SubmitVideo(ctx, opts)
}

// PollVideo delegates to the adapter.
func (c *UnifiedVideoClient) PollVideo(ctx context.Context, jobID string) (*VideoJob, error) {
	return c.adapter.PollVideo(ctx, jobID)
}

// WaitVideo loops PollVideo via pollAsync until terminal/timeout/cancel.
func (c *UnifiedVideoClient) WaitVideo(ctx context.Context, jobID string, opts PollOptions) (*VideoJob, error) {
	return pollAsync(ctx,
		func(ctx context.Context) (*VideoJob, error) { return c.adapter.PollVideo(ctx, jobID) },
		opts,
	)
}

// GenerateVideo is a convenience wrapper that submits a video job,
// waits for it to reach a terminal status (using opts.PollOptions),
// and — when opts.OutputDir is non-empty — downloads each
// completed video to disk via DownloadVideo. Returns the final
// VideoJob; callers must check job.Status to distinguish
// JobStatusCompleted from JobStatusFailed.
//
// Polling progress (ProgressPolling) is threaded through
// opts.OnProgress when non-nil; download progress
// (ProgressDownloading, ProgressComplete) fires after the job
// reaches JobStatusCompleted and OutputDir is set. Callers that
// need finer control over the three phases should call
// SubmitVideo / WaitVideo / DownloadVideo directly.
func (c *UnifiedVideoClient) GenerateVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error) {
	job, err := c.adapter.SubmitVideo(ctx, opts)
	if err != nil {
		return job, err
	}
	final, err := pollAsyncWithProgress(ctx,
		func(ctx context.Context) (*VideoJob, error) { return c.adapter.PollVideo(ctx, job.ID) },
		opts.PollOptions,
		opts.OnProgress,
	)
	if err != nil {
		return final, err
	}
	if opts.OutputDir != "" && final != nil && final.Status == JobStatusCompleted {
		if derr := DownloadVideo(ctx, final, opts); derr != nil {
			return final, derr
		}
	}
	return final, nil
}

func (c *UnifiedVideoClient) ProviderName() string { return c.adapter.ProviderName() }

var _ VideoClient = (*UnifiedVideoClient)(nil)
