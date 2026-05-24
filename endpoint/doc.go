// Package endpoint provides a unified, multi-provider LLM client for
// chat, image generation, video generation, and embeddings.
//
// UnifiedChatClient, UnifiedImageClient, and UnifiedVideoClient are
// thin orchestrators over provider-specific adapters — OpenAI-compatible
// (also covering vLLM, DeepSeek, Groq, OpenRouter), Anthropic, Z.ai, and
// Ollama — with auto-detection of the provider from a base URL, optional
// model and provider registries, override-wins extra headers and request
// body fields, and a pluggable RetryPolicy.
//
// Provider support matrix:
//
//	Provider     Chat  Stream  Image  Video  Embed
//	OpenAI       yes   yes     yes    yes    yes
//	OpenRouter   yes   yes     yes    yes    yes
//	Z.ai         yes   yes     yes    yes    no
//	Anthropic    yes   yes     no     no     no
//	Ollama       yes   yes     no     no     yes
//	vLLM/DeepSeek/Groq (OpenAI-compat)
//	             yes   yes     no     no     yes
//
// Providers without a given modality return ErrCapabilityNotSupported
// on the call.
//
// Quickstart:
//
//	client, err := endpoint.NewChatClient(endpoint.ChatClientConfig{
//	    EndpointURL: "https://api.openai.com",
//	    APIKey:      os.Getenv("OPENAI_API_KEY"),
//	})
//	resp, err := client.Chat(ctx, "gpt-4o", []endpoint.ChatMessage{
//	    {Role: "user", Content: "hello"},
//	}, nil)
//
// All four adapters return *EndpointError wrapping a typed sentinel
// for HTTP status-error paths. Match via errors.Is against
// ErrAuthenticationRequired (401), ErrRateLimited (429),
// ErrBadRequest (400, 422), ErrForbidden (403), ErrNotFound (404),
// ErrTimeout (408, 504), ErrServiceUnavailable (503), or
// ErrServerError (5xx catch-all).
//
// Reasoning-capable providers (currently Z.ai) emit a wire-level
// "thinking" control on each request. ModelProfile.ReasoningMode
// (off|auto|on) controls the wire shape per model; see ReasoningMode
// for off/auto/on semantics. The zero value (off) is the safe default
// for reasoning models like GLM-5.1.
//
// Image generation: NewImageClient constructs a UnifiedImageClient
// backed by one of three provider adapters — Z.ai (CogView-4 / GLM-Image,
// /api/paas/v4/images/generations), OpenAI (gpt-image-1 / dall-e-3,
// /v1/images/generations), or OpenRouter (chat-modality path through
// /v1/chat/completions; supports Gemini 2.5 Flash Image, FLUX, and
// others). The OpenRouter image parser tolerates the three response
// shapes observed in the wild: content as an array of blocks, content
// as null with image data in a sibling "images" field (Gemini's shape),
// and content as a plain string. Errors use the same sentinel set as
// the chat client (ErrRateLimited, ErrAuthenticationRequired, etc.)
// plus ErrCapabilityNotSupported for providers without an image adapter.
//
// Video generation: NewVideoClient constructs a UnifiedVideoClient
// backed by one of three asynchronous provider adapters — Z.ai
// (Vidu2 family / CogVideoX-3, /api/paas/v4/videos/generations +
// /api/paas/v4/async-result/{id}), OpenAI (Sora-2 / Sora-2-Pro,
// /v1/videos + /v1/videos/{id}/content streaming), or OpenRouter
// (normalized /v1/videos endpoint covering Sora 2 Pro, Veo 3.1, Veo
// 3.1 Lite, Seedance, Wan, Kling). Submit returns a *VideoJob;
// WaitVideo polls until the job reaches a terminal JobStatus via the
// shared pollAsync helper. GeneratedVideo.OpenContent provides a
// uniform, lazy io.ReadCloser regardless of whether the provider
// embeds a CDN URL or exposes a separate content endpoint. For
// OpenRouter providers that surface a list of authed
// "unsigned_urls" rather than embedded video URLs (e.g. Veo 3.1
// Lite), the library materializes these into GeneratedVideo entries
// automatically and forwards the configured Bearer token only when
// the content URL host matches the configured EndpointURL host —
// never to third-party CDNs. Errors use the shared sentinel set;
// ErrPollTimeout signals a client-side polling-loop budget
// exhaustion (distinct from the HTTP-level ErrTimeout).
//
// OpenRouter callers can use a single EndpointURL —
// "https://openrouter.ai/api" — across chat, image, and video clients.
// The per-modality path is supplied internally by the adapter.
//
// Download integration: image and video generation both support
// optional download-to-disk by setting ImageOptions.OutputDir or
// VideoOptions.OutputDir. When set, the library writes each
// generated artifact under that directory with the filename
// {prompt-slug}-v{N}.{ext}, where N is chosen to never overwrite an
// existing file (the library scans the directory and picks the next
// free version). Batch results (image N>1) use
// {prompt-slug}-v{N}-{idx}.{ext}.
//
// When OutputDir does not exist, the library returns an error
// wrapping fs.ErrNotExist unless CreateOutputDir=true, in which
// case it calls os.MkdirAll. Partial downloads are unlinked on
// failure so directory listings never show half-finished files.
//
// Both surfaces also accept an OnProgress callback. For images
// (synchronous) the callback receives ProgressDownloading events
// throttled at 256 KB or 200 ms (whichever first) and exactly one
// final ProgressComplete event. For videos (async) the callback
// additionally receives ProgressPolling events from the poll loop
// with the latest JobStatus and provider-reported Progress integer
// (-1 when the provider does not expose a numeric progress field).
//
// Callers who want stream-through semantics (e.g. forwarding bytes
// to another service without buffering) should leave OutputDir
// empty and use GeneratedVideo.OpenContent directly. OpenContent
// remains populated even when OutputDir is set, so the same caller
// can use both.
//
// See ChatClientConfig for the full set of options and CHANGELOG.md for
// version history.
package endpoint
