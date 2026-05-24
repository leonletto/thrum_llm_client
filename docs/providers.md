# Providers

This page documents per-provider setup, URL conventions, and known
quirks. The library auto-detects the provider from `EndpointURL` host;
pass `Provider: "..."` to override.

## Capability matrix

| Provider | Chat | Stream | Image | Video | Embed | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| **OpenAI** | ✓ | ✓ | ✓ | ✓ | ✓ | `gpt-image-1` / `dall-e-3`; `sora-2` / `sora-2-pro` |
| **OpenRouter** | ✓ | ✓ | ✓ | ✓ | ✓ | One base URL covers all modalities |
| **Z.ai** | ✓ (reasoning) | ✓ | ✓ | ✓ | — | GLM-5.1, CogView-4, Vidu2, CogVideoX-3 |
| **Anthropic** | ✓ | ✓ | — | — | — | Claude family |
| **Ollama** | ✓ | ✓ | — | — | ✓ | Local-first |
| **vLLM / DeepSeek / Groq** | ✓ | ✓ | — | — | ✓ | OpenAI-compatible |

Providers without a given modality return `endpoint.ErrCapabilityNotSupported`
on the call.

## OpenAI

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://api.openai.com",
    APIKey:      os.Getenv("OPENAI_API_KEY"),
})
```

Auto-detected host: `api.openai.com`.

The same base URL is used across chat, image (`/v1/images/generations`),
and video (`/v1/videos` + `/v1/videos/{id}/content`). The library
supplies the per-modality path internally.

## OpenRouter

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://openrouter.ai/api",
    APIKey:      os.Getenv("OPENROUTER_API_KEY"),
})
```

Auto-detected host: `openrouter.ai`.

**Important:** use the same base URL —
`https://openrouter.ai/api` — for chat, image, AND video clients. The
adapter supplies the right path per modality. (Earlier internal
versions required a different base URL for video; that footgun is
fixed.)

OpenRouter recommends two attribution headers; supply them via
`ExtraHeaders`:

```go
endpoint.ChatClientConfig{
    EndpointURL: "https://openrouter.ai/api",
    APIKey:      os.Getenv("OPENROUTER_API_KEY"),
    ExtraHeaders: map[string]string{
        "HTTP-Referer": "https://your-app.example.com",
        "X-Title":      "Your App Name",
    },
}
```

**Image response shapes.** OpenRouter's image-modality models return
three different response shapes in the wild. The parser handles all
three transparently: content as an array of blocks, content as `null`
with image data in a sibling `images` field (Gemini's shape), and
content as a plain string.

**Video unsigned URLs.** For models like `google/veo-3.1-lite` that
return `unsigned_urls` rather than structured `videos`, the library
materializes them into `GeneratedVideo` entries automatically. The
configured Bearer token is forwarded to the content URL **only** when
the URL host matches the configured `EndpointURL` host — third-party
CDN URLs never receive the credential.

## Z.ai

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://api.z.ai",
    APIKey:      os.Getenv("ZAI_API_KEY"),
})
```

Auto-detected host: `api.z.ai`.

### Reasoning mode

Z.ai's chat API takes a `thinking` control block. Reasoning-capable
models (e.g. GLM-5.1) interpret an **absent** `thinking` field as
"thinking enabled" and burn `MaxTokens` on hidden reasoning — producing
an empty `Content` with `finish_reason="length"`.

The library's default is to emit `{"type":"disabled"}` explicitly when
`EnableThinking=false`, which avoids that failure mode. Per-model
overrides live in `ModelProfile.ReasoningMode`:

| Mode | Wire shape | When to use |
| --- | --- | --- |
| `off` (default) | Explicit `{"type":"disabled"}` when `EnableThinking=false`; explicit `{"type":"enabled"}` when true. | Safe default. Reasoning models like GLM-5.1. |
| `auto` | Field omitted when `EnableThinking=false`; explicit `{"type":"enabled"}` when true. | Non-reasoning models that **reject** the explicit `disabled` form. |
| `on` | Always `{"type":"enabled"}`. | Always-reasoning models. |

YAML:

```yaml
profiles:
  - canonical_id: glm-5.1
    provider_models:
      zai: glm-5.1
    reasoning_mode: off  # explicit, but the default
```

See [registries.md](registries.md) for full registry setup.

### Image / video URLs

Z.ai returns signed URLs with ~30-day expiry. If you need persistence
beyond that, save the file via `OutputDir` (see [downloads.md](downloads.md)).

The video poll URL uses path-style (`/api/paas/v4/async-result/{id}`),
not query-style — handled internally.

## Anthropic

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://api.anthropic.com",
    APIKey:      os.Getenv("ANTHROPIC_API_KEY"),
})
```

Auto-detected host: `api.anthropic.com`.

Chat and streaming only. Image and video calls return
`ErrCapabilityNotSupported`. The adapter sets the required
`anthropic-version` header internally; override via `ExtraHeaders` if
you need a specific API version.

## Ollama

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "http://localhost:11434",  // or wherever
})
```

Auto-detected host: any URL matching `localhost`, `127.0.0.1`, or
`*.local`; otherwise pass `Provider: "ollama"` explicitly.

No API key required (it's a local server by default). Supports chat,
streaming, and embeddings.

## vLLM / DeepSeek / Groq (OpenAI-compatible)

These services expose the OpenAI Chat Completions API shape and are
served by the same adapter as OpenAI itself. Set the base URL and pass
`Provider: "openai"` (or rely on host auto-detection where possible):

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://api.deepseek.com",
    Provider:    "openai",  // explicit since the host isn't api.openai.com
    APIKey:      os.Getenv("DEEPSEEK_API_KEY"),
})
```

For vLLM and other self-hosted deployments, use the same pattern with
the local URL. Provider-specific knobs (vLLM's `top_k`, etc.) go
through `ExtraBody`:

```go
endpoint.ChatClientConfig{
    EndpointURL: "http://vllm.internal:8000",
    Provider:    "openai",
    ExtraBody:   map[string]any{"top_k": 40},
}
```

## ExtraHeaders and ExtraBody — override-wins

Both `ExtraHeaders` and `ExtraBody` use **override-wins** merging:
your value shadows any adapter default with the same key. This lets you
override `Authorization`, `Content-Type`, etc. on a per-client basis
without forking the adapter.
