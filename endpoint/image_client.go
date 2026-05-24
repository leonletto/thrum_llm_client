package endpoint

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ImageClient is the public-facing image-generation surface.
// Callers receive a *UnifiedImageClient from NewImageClient that
// satisfies this interface.
type ImageClient interface {
	GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error)
	ProviderName() string
}

// ImageClientAdapter is implemented by per-provider image adapters
// (imageZaiClient, imageOpenAIClient, imageOpenRouterClient).
// Callers should not depend on this interface directly; use
// ImageClient.
type ImageClientAdapter interface {
	GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error)
	ProviderName() string
}

// ImageClientConfig configures the UnifiedImageClient.
type ImageClientConfig struct {
	EndpointURL string
	Provider    string // optional; auto-detected from URL if empty
	APIKey      string

	// HTTPClient overrides the default transport. nil → adapter default.
	HTTPClient *http.Client

	// ExtraHeaders are applied to every outbound request with
	// override-wins semantics, mirroring ChatClientConfig.ExtraHeaders.
	ExtraHeaders map[string]string

	// ExtraBody is merged into the outbound JSON body with
	// override-wins semantics, mirroring ChatClientConfig.ExtraBody.
	// (Per-call ExtraBody on ImageOptions stacks on top.)
	ExtraBody map[string]any
}

// UnifiedImageClient wraps a provider-specific image adapter.
type UnifiedImageClient struct {
	adapter ImageClientAdapter
}

// NewImageClient constructs an image client with provider auto-detection.
func NewImageClient(cfg ImageClientConfig) (*UnifiedImageClient, error) {
	if cfg.EndpointURL == "" {
		return nil, fmt.Errorf("EndpointURL is required")
	}
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg.EndpointURL)
		// detectProvider returns "openai" for openrouter.ai URLs
		// (correct for chat). Image gen on OpenRouter uses a
		// different wire shape (chat-modality), so re-route here.
		if provider == "openai" && strings.Contains(strings.ToLower(cfg.EndpointURL), "openrouter.ai") {
			provider = "openrouter"
		}
	}

	adapter, err := createImageAdapter(provider, cfg.EndpointURL, cfg.APIKey, cfg.ExtraHeaders)
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
	return &UnifiedImageClient{adapter: adapter}, nil
}

// createImageAdapter returns the right adapter for the provider.
// Returns ErrCapabilityNotSupported for providers without image
// generation support (anthropic, ollama).
func createImageAdapter(provider, url, apiKey string, extraHeaders map[string]string) (ImageClientAdapter, error) {
	switch provider {
	case "zai":
		return newImageZaiClient(url, apiKey, extraHeaders), nil
	case "openai", "openai-compat":
		return newImageOpenAIClient(url, apiKey, extraHeaders), nil
	case "openrouter":
		return newImageOpenRouterClient(url, apiKey, extraHeaders), nil
	case "anthropic", "ollama":
		return nil, fmt.Errorf("provider %q image generation: %w", provider, ErrCapabilityNotSupported)
	default:
		// Unknown providers fall through to OpenAI-compat (mirrors
		// the chat-side default) — caller can specialize later.
		return newImageOpenAIClient(url, apiKey, extraHeaders), nil
	}
}

// GenerateImage delegates to the underlying adapter, then — when
// opts.OutputDir is set — downloads each returned image to disk
// and populates Images[i].LocalPath. Zero-value OutputDir skips the
// download step entirely.
func (c *UnifiedImageClient) GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error) {
	res, err := c.adapter.GenerateImage(ctx, opts)
	if err != nil {
		return res, err
	}
	if opts.OutputDir == "" || res == nil || len(res.Images) == 0 {
		return res, nil
	}

	httpClient := http.DefaultClient
	if hcp, ok := c.adapter.(httpClientProvider); ok && hcp.HTTPClient() != nil {
		httpClient = hcp.HTTPClient()
	}

	if err := ensureOutputDir(opts.OutputDir, opts.CreateOutputDir); err != nil {
		return res, err
	}

	// Batch-atomic versioning: pick N ONCE for the whole batch so
	// every member shares it ({slug}-vN-1.{ext}, {slug}-vN-2.{ext},
	// ...). The per-image helper bumps locally only on the rare
	// EEXIST race with a concurrent writer.
	//
	// Caveat: extension is per-image (URL vs base64 path differ),
	// but provider batches today are homogeneous — Z.ai/OpenAI/
	// OpenRouter all return one MIME per response. We use the
	// first image's extension to scan. Future providers mixing
	// MIMEs within a batch would need per-extension version
	// scans; revisit this when one appears.
	slug := slugify(opts.Prompt)
	firstExt := extForGeneratedImage(res.Images[0])
	version, err := pickVersion(opts.OutputDir, slug, firstExt)
	if err != nil {
		return res, fmt.Errorf("image download: pick version: %w", err)
	}

	batched := len(res.Images) > 1
	for i := range res.Images {
		idx := -1
		if batched {
			idx = i + 1
		}
		out, derr := downloadGeneratedImage(ctx, httpClient, opts, res.Images[i], version, idx)
		if derr != nil {
			return res, derr
		}
		res.Images[i] = out
	}
	return res, nil
}

// extForGeneratedImage returns the best-guess filename extension
// for an image without making any HTTP request. base64 payloads
// default to "png"; URL-bearing images use the URL path extension
// when present, else "png" as the safest provider-default.
func extForGeneratedImage(img GeneratedImage) string {
	if len(img.Bytes) > 0 {
		return "png"
	}
	if e := extFromURL(img.URL); e != "" {
		return e
	}
	return "png"
}

// httpClientProvider is an optional adapter-side accessor exposing
// the http.Client used for outbound provider calls. UnifiedImageClient
// and UnifiedVideoClient consume it to thread the same client into
// download requests so existing transport customizations (proxy,
// timeouts) apply uniformly.
type httpClientProvider interface {
	HTTPClient() *http.Client
}

// ProviderName returns the underlying provider's identifier.
func (c *UnifiedImageClient) ProviderName() string {
	return c.adapter.ProviderName()
}

var _ ImageClient = (*UnifiedImageClient)(nil)
