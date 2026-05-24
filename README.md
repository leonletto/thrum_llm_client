# thrum_llm_client

[![License](https://img.shields.io/github/license/leonletto/thrum_llm_client)](LICENSE)
[![CI](https://github.com/leonletto/thrum_llm_client/actions/workflows/ci.yml/badge.svg)](https://github.com/leonletto/thrum_llm_client/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/leonletto/thrum_llm_client)](https://github.com/leonletto/thrum_llm_client/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/leonletto/thrum_llm_client.svg)](https://pkg.go.dev/github.com/leonletto/thrum_llm_client)
[![Go Report Card](https://goreportcard.com/badge/github.com/leonletto/thrum_llm_client)](https://goreportcard.com/report/github.com/leonletto/thrum_llm_client)
[![Go Version](https://img.shields.io/github/go-mod/go-version/leonletto/thrum_llm_client)](go.mod)

Multi-provider LLM endpoint client for Go. One unified API across chat, image generation, video generation, and embeddings — backed by OpenAI-compatible providers (including vLLM, DeepSeek, Groq, OpenRouter), Anthropic, Z.ai, and Ollama. Auto-detects provider from URL, handles retries, supports streaming, writes generated artifacts to disk on request, and surfaces typed errors for control flow.

## Module path

`github.com/leonletto/thrum_llm_client`

## Package

```go
import "github.com/leonletto/thrum_llm_client/endpoint"
```

## Installation

```sh
go get github.com/leonletto/thrum_llm_client@latest
```

## Provider support

| Provider | Chat | Stream | Image | Video | Embed | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| **OpenAI** | ✓ | ✓ | ✓ | ✓ | ✓ | `gpt-image-1` / `dall-e-3` for image; `sora-2` / `sora-2-pro` for video |
| **OpenRouter** | ✓ | ✓ | ✓ | ✓ | ✓ | One `EndpointURL = "https://openrouter.ai/api"` covers all modalities. Image: Gemini, FLUX, etc. Video: Veo, Sora, Seedance, Wan, Kling |
| **Z.ai** | ✓ (reasoning) | ✓ | ✓ | ✓ | — | GLM-5.1 chat with `ReasoningMode` control. CogView-4 / GLM-Image. Vidu2 family + CogVideoX-3 |
| **Anthropic** | ✓ | ✓ | — | — | — | Claude family. Image/video not exposed by provider |
| **Ollama** | ✓ | ✓ | — | — | ✓ | Local-first; chat + embeddings |
| **vLLM / DeepSeek / Groq** | ✓ | ✓ | — | — | ✓ | OpenAI-compatible adapter; embeddings where the provider supports them |

Providers without a given modality return `endpoint.ErrCapabilityNotSupported` on the call.

## Reasoning-mode policy (Z.ai)

Reasoning-capable providers (currently Z.ai) emit a wire-level "thinking" control on each request. `ModelProfile.ReasoningMode` controls the wire shape per model:

- `off` (default): emit explicit `{"type":"disabled"}` when `EnableThinking=false`. Safe for reasoning models like GLM-5.1.
- `auto`: omit the field when disabled. Use only for non-reasoning models that reject explicit `disabled`.
- `on`: force `{"type":"enabled"}` regardless of caller — for always-reasoning models.

YAML:

```yaml
profiles:
  - canonical_id: glm-5.1
    provider_models: { zai: glm-5.1 }
    reasoning_mode: off  # explicit, but the default
```

## Typed errors

All four provider adapters return `*endpoint.EndpointError` wrapping a
typed sentinel for HTTP status-error paths. Use `errors.Is`:

```go
_, err := client.Chat(ctx, model, msgs, nil)
switch {
case errors.Is(err, endpoint.ErrAuthenticationRequired): // 401
case errors.Is(err, endpoint.ErrRateLimited):            // 429 — back off
case errors.Is(err, endpoint.ErrBadRequest):             // 400, 422
case errors.Is(err, endpoint.ErrForbidden):              // 403
case errors.Is(err, endpoint.ErrNotFound):               // 404
case errors.Is(err, endpoint.ErrTimeout):                // 408, 504
case errors.Is(err, endpoint.ErrServiceUnavailable):     // 503 — retry with backoff
case errors.Is(err, endpoint.ErrServerError):            // 5xx catch-all
}
```

The provider-supplied human-readable message (when present) is preserved
in the wrapped error's text for logs, while `errors.Is` matches the
typed sentinel for control flow.

## Image generation

Three providers supported: Z.ai (CogView-4, GLM-Image), OpenAI (gpt-image-1, dall-e-3), OpenRouter (Gemini 2.5 Flash Image / "Nano Banana", FLUX, etc.).

For OpenRouter image and video, use the same base URL — `https://openrouter.ai/api` — across all modalities (chat, image, video). The library handles the per-modality path internally.

```go
client, err := endpoint.NewImageClient(endpoint.ImageClientConfig{
    EndpointURL: "https://api.z.ai",
    APIKey:      os.Getenv("ZAI_API_KEY"),
})
res, err := client.GenerateImage(ctx, endpoint.ImageOptions{
    Model:  "cogView-4-250304",
    Prompt: "A kitten on a sunny windowsill",
    Size:   "1024x1024",
})
url := res.Images[0].URL  // 30-day expiry on Z.ai; ~1h on OpenAI dall-e
```

Errors use the same `errors.Is` sentinel set as chat:

```go
if errors.Is(err, endpoint.ErrRateLimited) {
    // back off
}
```

## Video generation

Three providers supported: Z.ai (Vidu2 family, CogVideoX-3), OpenAI (Sora-2, Sora-2-Pro), OpenRouter (Sora 2 Pro / Veo 3.1 / Seedance / Wan / Kling via their normalized API).

```go
client, err := endpoint.NewVideoClient(endpoint.VideoClientConfig{
    EndpointURL: "https://api.openai.com",
    APIKey:      os.Getenv("OPENAI_API_KEY"),
})
job, err := client.SubmitVideo(ctx, endpoint.VideoOptions{
    Model:    "sora-2-pro",
    Prompt:   "A cat surfing a wave at sunset",
    Size:     "1280x720",
    Duration: 16,
})
job, err = client.WaitVideo(ctx, job.ID, endpoint.PollOptions{
    Interval: 10 * time.Second,
    MaxWait:  15 * time.Minute,
})
if job.Status != endpoint.JobStatusCompleted {
    return fmt.Errorf("job ended in %s: %s", job.Status, job.Error)
}
rc, err := job.Videos[0].OpenContent(ctx)
defer rc.Close()
io.Copy(file, rc)
```

`OpenContent` works uniformly across providers — Z.ai/OpenRouter HTTP-GET the embedded URL; OpenAI streams from a separate content endpoint. Caller never branches on provider.

`WaitVideo` returns `endpoint.ErrPollTimeout` when `PollOptions.MaxWait` is exceeded — distinct from the HTTP-level `ErrTimeout` (408/504). Context cancellation propagates the wrapped `context.Canceled` / `context.DeadlineExceeded` unchanged.

## Saving generated artifacts to disk

Both image and video generation support an opt-in download to a caller-supplied directory. When `OutputDir` is set the library writes each artifact under that directory with a predictable, versioned filename — no overwrites, no surprises:

```go
imgClient, _ := endpoint.NewImageClient(cfg)
res, err := imgClient.GenerateImage(ctx, endpoint.ImageOptions{
    Model:     "cogview-4-250304",
    Prompt:    "a red cat at dusk",
    OutputDir: "/var/lib/myapp/images",
    // CreateOutputDir: true,  // uncomment to auto-mkdir
    OnProgress: func(e endpoint.ProgressEvent) {
        log.Printf("phase=%s percent=%d", e.Phase, e.Percent)
    },
})
// res.Images[0].LocalPath -> "/var/lib/myapp/images/a-red-cat-at-dusk-v1.png"
```

Filenames use `{prompt-slug}-v{N}.{ext}`, with `N` picked to never collide with an existing file. Image batches (N>1) use `{prompt-slug}-v{N}-{idx}.{ext}`. Partial downloads are unlinked on failure. When `OutputDir` is missing and `CreateOutputDir` is unset, the call returns an error satisfying `errors.Is(err, fs.ErrNotExist)`.

For video, `UnifiedVideoClient.GenerateVideo` composes Submit + Wait + Download in one call. `OnProgress` fires for both polling (`ProgressPolling` with the latest `JobStatus`) and download (`ProgressDownloading` / `ProgressComplete`). The lazy `GeneratedVideo.OpenContent` closure remains populated even when `OutputDir` is set, so stream-through callers can choose between path-based and stream-based access. Callers using the three-phase Submit / Poll / Wait API directly can still get library-managed downloads via `endpoint.DownloadVideo(ctx, job, opts)`.

## Clean Room Development

This project is developed following clean room practices:

- Personal devices and accounts only
- Public documentation and open-source libraries only
- Zero employer code, data, or confidential information
