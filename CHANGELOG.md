# Changelog

All notable changes to this module will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0]

### Added

- Initial public release. Single Go package (`endpoint`) providing a
  unified, multi-provider LLM client across four modalities:

  - **Chat** (with streaming) — OpenAI-compatible (also covering
    vLLM, DeepSeek, Groq, OpenRouter), Anthropic, Z.ai, Ollama.
  - **Image generation** — Z.ai (CogView-4, GLM-Image), OpenAI
    (gpt-image-1, dall-e-3), OpenRouter (Gemini 2.5 Flash Image,
    FLUX, etc.).
  - **Video generation** (async Submit + Poll/Wait + optional
    Download) — Z.ai (Vidu2 family, CogVideoX-3), OpenAI (Sora-2,
    Sora-2-Pro), OpenRouter (Sora 2 Pro, Veo 3.1, Seedance, Wan,
    Kling).
  - **Embeddings** — OpenAI-compatible providers and Ollama.

- Provider auto-detection from `EndpointURL` host; explicit
  `Provider` override.
- Typed sentinel errors (`ErrAuthenticationRequired`, `ErrRateLimited`,
  `ErrBadRequest`, `ErrForbidden`, `ErrNotFound`, `ErrTimeout`,
  `ErrServiceUnavailable`, `ErrServerError`, `ErrPollTimeout`,
  `ErrCapabilityNotSupported`, `ErrZaiEmptyToolCall`) wrapped by
  `*EndpointError` and matched via `errors.Is`.
- Pluggable `RetryPolicy` with composable `RetryPredicate`s.
  `DefaultRetryPolicy()` retries the Z.ai empty-tool-call failure
  mode once; `&RetryPolicy{}` opts out.
- Override-wins composition: `ExtraHeaders` and `ExtraBody` merge
  into every outbound request with caller values shadowing adapter
  defaults.
- Optional `ProviderRegistry` and `ModelRegistry` (YAML-loaded via
  `config_loader.go`) for canonical-name resolution across providers.
- `ReasoningMode` (`off|auto|on`) per-model wire-shape policy for
  reasoning-capable providers (currently Z.ai's `thinking` field).
  Default `off` emits explicit `{"type":"disabled"}` — safe for
  reasoning models like GLM-5.1.
- Tool-call support across non-streaming and streaming responses
  (delta accumulation by index for Z.ai).
- Optional download-to-disk for image and video results via
  `ImageOptions.OutputDir` / `VideoOptions.OutputDir`. Filenames
  follow `{prompt-slug}-v{N}.{ext}` (or
  `{prompt-slug}-v{N}-{idx}.{ext}` for image batches); `N` is
  selected to never overwrite an existing file. Partial downloads
  are unlinked on failure. Missing dir returns an error wrapping
  `fs.ErrNotExist` unless `CreateOutputDir=true`.
- `OnProgress` callback receiving `ProgressEvent`s during polling
  (`ProgressPolling` with the latest `JobStatus`) and download
  (`ProgressDownloading` / `ProgressComplete`), throttled at 256 KB
  or 200 ms.
- `UnifiedVideoClient.GenerateVideo` convenience wrapper composing
  Submit + Wait + Download in one call. Standalone
  `endpoint.DownloadVideo(ctx, job, opts)` for callers using the
  three-phase API directly.
- `GeneratedVideo.OpenContent` providing a uniform, lazy
  `io.ReadCloser` regardless of whether the provider embeds a CDN
  URL or exposes a separate content endpoint. For OpenRouter
  providers returning `unsigned_urls` (e.g. `google/veo-3.1-lite`),
  the configured Bearer token is forwarded only when the content
  URL host matches the configured `EndpointURL` host —
  third-party CDN URLs never receive the credential.
- Live-API end-to-end smoke suite under `tests/e2e/`, build-tag
  gated (`//go:build e2e`) so `go test ./...` is unaffected. Entry
  point: `make e2e`. Tests skip cleanly when the relevant API key
  is unset.

### Notes

- Sole runtime dependency: `gopkg.in/yaml.v3`.
- Module path: `github.com/leonletto/thrum_llm_client`.
- Requires Go 1.25 or newer.
