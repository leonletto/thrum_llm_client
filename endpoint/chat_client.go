// Package endpoint provides LLM endpoint management with multi-provider support.
package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// streamScannerBufferMax is the maximum SSE / NDJSON line size accepted by
// the streaming scanners across all providers. Default bufio.Scanner caps
// at 64 KB which is too small for chunks carrying base64 images or long
// tool-call arguments. 8 MB is the same ceiling Anthropic's own SDK uses.
const streamScannerBufferMax = 8 * 1024 * 1024

// ChatClientConfig configures the UnifiedChatClient.
type ChatClientConfig struct {
	// EndpointURL is the base URL for the LLM endpoint
	EndpointURL string

	// Provider explicitly sets the provider type. If empty, auto-detected from URL.
	// Valid values: "openai", "anthropic", "zai", "ollama"
	Provider string

	// APIKey is the authentication key for the provider
	APIKey string

	// ModelRegistry is an optional registry for canonical model name resolution
	ModelRegistry *ModelRegistry

	// ChatPath overrides the default chat completions path for this provider.
	// If empty, the adapter's default is used (e.g., "/v1/chat/completions" for OpenAI).
	ChatPath string

	// ExtraHeaders are additional HTTP headers applied to every outbound
	// request (Chat and Stream paths) across all four providers — openai,
	// anthropic, zai, and ollama. Override-wins on conflict: if a key here
	// collides with a provider default (Authorization, Content-Type,
	// x-api-key, anthropic-version, etc.), the caller-supplied value
	// shadows the default. Use for provider-specific extensions
	// (OpenRouter's HTTP-Referer / X-Title, custom proxy auth, etc.).
	ExtraHeaders map[string]string

	// Registry-based resolution: set both to resolve provider config by name.
	// When set, EndpointURL/Provider/APIKey are resolved from the registry.
	ProviderName     string
	ProviderRegistry *ProviderRegistry

	// HTTPClient overrides the default transport. When nil, the provider's
	// hardcoded default (with sensible timeouts) is used.
	HTTPClient *http.Client

	// ExtraBody contains additional JSON fields merged into every chat request
	// body. Override-wins on conflict — caller-supplied values shadow the
	// adapter's defaults. Use for provider-specific extensions (vLLM's top_k,
	// OpenRouter's provider routing, Together's safety_model, etc.).
	ExtraBody map[string]any

	// RetryPolicy controls transport-layer retry behavior. When nil, the
	// shipping DefaultRetryPolicy() is applied (1 retry on the zai
	// empty / malformed tool-call failure mode). To explicitly opt out
	// of retries, pass a non-nil &RetryPolicy{} (MaxRetries == 0).
	RetryPolicy *RetryPolicy
}

// extraBodyConfigurable is implemented by adapters that support runtime
// override of HTTP transport, extra request body fields, and extra
// HTTP headers.
type extraBodyConfigurable interface {
	setHTTPClient(*http.Client)
	setExtraBody(map[string]any)
	setExtraHeaders(map[string]string)
}

// reasoningModeConfigurable is implemented by adapters that consume a
// per-call ReasoningMode resolver (currently only the Z.ai adapter).
// The resolver is invoked with the provider-specific model name at
// request build time. When no registry is configured the resolver
// returns ReasoningModeOff, which causes adapters that wire it (e.g.
// Z.ai) to emit explicit {"type":"disabled"} when
// EnableThinking=false.
type reasoningModeConfigurable interface {
	setReasoningModeResolver(func(model string) ReasoningMode)
}

// buildReasoningModeResolver constructs a closure that, given a
// provider-specific model name, returns the configured ReasoningMode
// for that model. Lookup uses ModelRegistry's reverse index (case-
// insensitive on the provider-specific model name). If the registry
// is nil or the model is not registered for the given provider,
// ReasoningModeOff is returned. The actual lookup happens inside
// ModelRegistry.ResolveReasoningMode under a single RLock so the
// cross-provider membership check is race-free.
func buildReasoningModeResolver(reg *ModelRegistry, provider string) func(model string) ReasoningMode {
	if reg == nil {
		return func(string) ReasoningMode { return ReasoningModeOff }
	}
	return func(providerModel string) ReasoningMode {
		return reg.ResolveReasoningMode(providerModel, provider)
	}
}

// applyExtraHeaders writes caller-supplied headers onto req, overriding
// any pre-set provider defaults (override-wins). Empty/nil map is a no-op.
func applyExtraHeaders(req *http.Request, extra map[string]string) {
	for k, v := range extra {
		req.Header.Set(k, v)
	}
}

// mergeExtraBody merges extra into the JSON object encoded in body using
// override-wins semantics (extra shadows existing keys). Returns body
// unchanged if extra is empty.
func mergeExtraBody(body []byte, extra map[string]any) ([]byte, error) {
	if len(extra) == 0 {
		return body, nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("merge extra body: %w", err)
	}
	for k, v := range extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// UnifiedChatClient wraps provider-specific adapters with automatic detection and model resolution.
type UnifiedChatClient struct {
	adapter              ChatClientAdapter
	modelRegistry        *ModelRegistry
	providerCapabilities *ProviderCapabilities // nil when created without registry
	retryPolicy          *RetryPolicy          // nil disables retry
}

// NewChatClient creates a new UnifiedChatClient with automatic provider detection.
func NewChatClient(cfg ChatClientConfig) (*UnifiedChatClient, error) {
	var providerCaps *ProviderCapabilities

	// Registry-based resolution path
	if cfg.ProviderName != "" && cfg.ProviderRegistry != nil {
		provCfg, err := cfg.ProviderRegistry.Get(cfg.ProviderName)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", cfg.ProviderName, err)
		}

		// Lazy API key resolution
		apiKey := ""
		if provCfg.APIKeyEnv != "" {
			apiKey = os.Getenv(provCfg.APIKeyEnv)
			if apiKey == "" {
				return nil, fmt.Errorf("provider %q: env var %s is not set", cfg.ProviderName, provCfg.APIKeyEnv)
			}
		}

		caps := provCfg.Capabilities
		providerCaps = &caps

		cfg.EndpointURL = provCfg.Endpoint
		cfg.Provider = provCfg.Type
		cfg.APIKey = apiKey
		cfg.ChatPath = provCfg.ChatPath
		// Merge registry headers with caller-supplied headers; caller wins on
		// conflict (override-wins, matching the rest of the package).
		if len(provCfg.ExtraHeaders) > 0 {
			merged := make(map[string]string, len(provCfg.ExtraHeaders)+len(cfg.ExtraHeaders))
			for k, v := range provCfg.ExtraHeaders {
				merged[k] = v
			}
			for k, v := range cfg.ExtraHeaders {
				merged[k] = v
			}
			cfg.ExtraHeaders = merged
		}
	}

	if cfg.EndpointURL == "" {
		return nil, fmt.Errorf("EndpointURL is required")
	}

	// Determine provider
	provider := cfg.Provider
	if provider == "" {
		provider = detectProvider(cfg.EndpointURL)
	}

	// Create appropriate adapter
	adapter, err := createAdapter(provider, cfg.EndpointURL, cfg.APIKey, cfg.ChatPath, cfg.ExtraHeaders)
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

	if rmc, ok := adapter.(reasoningModeConfigurable); ok {
		rmc.setReasoningModeResolver(buildReasoningModeResolver(cfg.ModelRegistry, provider))
	}

	// Apply default retry policy when caller passes nil. An explicit
	// non-nil &RetryPolicy{} (MaxRetries == 0) is the documented opt-out.
	policy := cfg.RetryPolicy
	if policy == nil {
		policy = DefaultRetryPolicy()
	}

	return &UnifiedChatClient{
		adapter:              adapter,
		modelRegistry:        cfg.ModelRegistry,
		providerCapabilities: providerCaps,
		retryPolicy:          policy,
	}, nil
}

// detectProvider determines the provider from URL patterns.
func detectProvider(url string) string {
	lower := strings.ToLower(url)

	// Anthropic detection
	if strings.Contains(lower, "api.anthropic.com") {
		return "anthropic"
	}

	// Z.ai detection
	if strings.Contains(lower, "api.z.ai") || strings.Contains(lower, "z.ai") {
		return "zai"
	}

	// OpenRouter detection (OpenAI-compatible API)
	if strings.Contains(lower, "openrouter.ai") {
		return "openai"
	}

	// Ollama detection (common ports)
	if strings.Contains(lower, ":11434") || strings.Contains(lower, ":11445") {
		return "ollama"
	}

	// Default to OpenAI-compatible (works for OpenAI, vLLM, DeepSeek, Groq)
	return "openai"
}

// createAdapter creates the appropriate ChatClientAdapter for the provider.
func createAdapter(provider, url, apiKey, chatPath string, extraHeaders map[string]string) (ChatClientAdapter, error) {
	switch provider {
	case "anthropic":
		return NewAnthropicClient(url, apiKey), nil
	case "zai":
		client := NewZaiClientWithEndpoint(apiKey, url)
		client.chatPath = chatPath
		return client, nil
	case "ollama":
		return NewOllamaClient(url, chatPath), nil
	case "openai", "openai-compat", "vllm", "deepseek", "groq", "openrouter":
		return NewOpenAICompatClient(url, apiKey, chatPath, extraHeaders, provider), nil
	default:
		// Unknown provider - use OpenAI-compatible as fallback
		return NewOpenAICompatClient(url, apiKey, chatPath, extraHeaders, provider), nil
	}
}

// resolveModel translates canonical model names to provider-specific names.
func (c *UnifiedChatClient) resolveModel(model string) string {
	if c.modelRegistry == nil {
		return model
	}

	providerName := c.adapter.ProviderName()
	resolved, err := c.modelRegistry.ResolveModel(model, providerName)
	if err != nil {
		// Model not in registry - use as-is (likely already provider-specific)
		return model
	}
	return resolved
}

// Chat sends a chat completion request and returns the response.
//
// When a RetryPolicy is configured (the default), each attempt is offered
// to the policy's predicates; the first firing predicate triggers a retry,
// up to RetryPolicy.MaxRetries additional attempts.
func (c *UnifiedChatClient) Chat(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions) (*ChatResponse, error) {
	resolvedModel := c.resolveModel(model)
	provider := c.adapter.ProviderName()

	req := ChatRequest{
		Provider: provider,
		Model:    resolvedModel,
		Messages: messages,
		Options:  opts,
		Stream:   false,
	}

	var resp *ChatResponse
	var err error
	if c.retryPolicy == nil {
		// No policy — single attempt.
		resp, err = c.adapter.Chat(ctx, resolvedModel, messages, opts)
	} else {
		for attempt := 0; attempt <= c.retryPolicy.MaxRetries; attempt++ {
			start := time.Now()
			resp, err = c.adapter.Chat(ctx, resolvedModel, messages, opts)
			latency := time.Since(start)

			retry, predName, reason := c.retryPolicy.shouldRetry(req, resp, err)
			if !retry || attempt == c.retryPolicy.MaxRetries {
				break
			}
			if c.retryPolicy.OnRetry != nil {
				c.retryPolicy.OnRetry(RetryEvent{
					Attempt:       attempt + 1,
					PredicateName: predName,
					Reason:        reason,
					LatencyDelta:  latency,
				})
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", provider, err)
	}
	return resp, nil
}

// ChatStream sends a streaming chat request and invokes callback for each chunk.
// Returns finalized tool-calls from the stream (nil if none or not supported by provider).
//
// Retry semantics for streams: a configured RetryPolicy may retry the
// entire stream, but only when no chunk has yet been delivered to the
// caller's callback. Once the first chunk fires, the stream is committed
// — subsequent attempts are not retried, and the final result is returned
// as-is.
func (c *UnifiedChatClient) ChatStream(ctx context.Context, model string, messages []ChatMessage, opts *ChatOptions, callback func(chunk string) error) ([]ToolCall, error) {
	if c.providerCapabilities != nil && !c.providerCapabilities.Streaming {
		return nil, fmt.Errorf("%s: provider does not support streaming", c.adapter.ProviderName())
	}
	resolvedModel := c.resolveModel(model)
	provider := c.adapter.ProviderName()

	req := ChatRequest{
		Provider: provider,
		Model:    resolvedModel,
		Messages: messages,
		Options:  opts,
		Stream:   true,
	}

	var toolCalls []ToolCall
	var err error
	if c.retryPolicy == nil {
		// No policy — single attempt.
		toolCalls, err = c.adapter.ChatStream(ctx, resolvedModel, messages, opts, callback)
	} else {
		for attempt := 0; attempt <= c.retryPolicy.MaxRetries; attempt++ {
			// Wrap callback to detect first delivery; once tripped, retry is forbidden.
			var delivered bool
			wrapped := func(chunk string) error {
				delivered = true
				return callback(chunk)
			}

			start := time.Now()
			toolCalls, err = c.adapter.ChatStream(ctx, resolvedModel, messages, opts, wrapped)
			latency := time.Since(start)

			// Build a synthetic response for predicate inspection on streams.
			// Stream paths don't return a ChatResponse, so we surface the
			// tool-call vector + a finish_reason hint so predicates have
			// uniform footing.
			var pseudoResp *ChatResponse
			if err == nil {
				fr := ""
				if len(toolCalls) > 0 {
					fr = "tool_calls"
				}
				pseudoResp = &ChatResponse{ToolCalls: toolCalls, FinishReason: fr}
			}

			retry, predName, reason := c.retryPolicy.shouldRetry(req, pseudoResp, err)
			if !retry || attempt == c.retryPolicy.MaxRetries || delivered {
				break
			}
			if c.retryPolicy.OnRetry != nil {
				c.retryPolicy.OnRetry(RetryEvent{
					Attempt:       attempt + 1,
					PredicateName: predName,
					Reason:        reason,
					LatencyDelta:  latency,
				})
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", provider, err)
	}
	return toolCalls, nil
}

// Embed generates embeddings for the given texts.
func (c *UnifiedChatClient) Embed(ctx context.Context, model string, texts []string) ([][]float64, error) {
	if c.providerCapabilities != nil && !c.providerCapabilities.Embedding {
		return nil, fmt.Errorf("%s: provider does not support embedding", c.adapter.ProviderName())
	}
	resolvedModel := c.resolveModel(model)

	embeddings, err := c.adapter.Embed(ctx, resolvedModel, texts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.adapter.ProviderName(), err)
	}

	return embeddings, nil
}

// ProviderName returns the name of the underlying provider.
func (c *UnifiedChatClient) ProviderName() string {
	return c.adapter.ProviderName()
}

// Compile-time interface verification
var _ ChatClient = (*UnifiedChatClient)(nil)
