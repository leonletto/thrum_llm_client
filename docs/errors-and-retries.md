# Errors and retries

This library returns typed sentinel errors for all HTTP-status failure
paths and ships a pluggable `RetryPolicy` for transport-layer retries.

## Typed errors

All provider adapters (chat, image, video) wrap HTTP failures in
`*endpoint.EndpointError`, which embeds a typed sentinel. Match the
sentinel via `errors.Is`:

```go
import "errors"

resp, err := client.Chat(ctx, "gpt-4o", msgs, nil)
switch {
case errors.Is(err, endpoint.ErrAuthenticationRequired):
    // 401 — bad or missing API key
case errors.Is(err, endpoint.ErrRateLimited):
    // 429 — back off
case errors.Is(err, endpoint.ErrBadRequest):
    // 400, 422 — caller-side bug, do not retry
case errors.Is(err, endpoint.ErrForbidden):
    // 403
case errors.Is(err, endpoint.ErrNotFound):
    // 404 — unknown model, missing job, etc.
case errors.Is(err, endpoint.ErrTimeout):
    // 408, 504 — wire-level timeout
case errors.Is(err, endpoint.ErrServiceUnavailable):
    // 503 — retry with backoff
case errors.Is(err, endpoint.ErrServerError):
    // 5xx catch-all
}
```

The provider's human-readable message (when present) is preserved in
the wrapped error's text for logs:

```go
log.Printf("chat call failed: %v", err)
// 2026-05-24 ... chat call failed: openai: rate_limit_exceeded: ...
```

### Modality-specific sentinels

| Sentinel | Where | Meaning |
| --- | --- | --- |
| `ErrPollTimeout` | `WaitVideo`, `GenerateVideo` | `PollOptions.MaxWait` exhausted before the job reached a terminal status. Distinct from the HTTP-level `ErrTimeout` (408/504). |
| `ErrCapabilityNotSupported` | `NewImageClient`, `NewVideoClient`, `Embed` | The configured provider has no adapter for this modality (e.g., Anthropic image generation). |
| `ErrZaiEmptyToolCall` | `Chat`, `ChatStream` | Z.ai returned a tool-call response with empty or malformed arguments. The default retry policy retries this once. |

### Configuration-level sentinels

These surface from constructors and registry lookups, not from
provider calls:

| Sentinel | Meaning |
| --- | --- |
| `ErrInvalidEndpoint` | `EndpointURL` empty or unparseable. |
| `ErrInvalidConfiguration` | Constructor received contradictory or incomplete config. |
| `ErrProviderNotFound` | `ProviderRegistry.Get` lookup miss. |
| `ErrProviderNotConfigured` | Registry-resolved provider has no `APIKeyEnv` set or the env var is empty. |
| `ErrModelNotFound`, `ErrModelProfileNotFound` | `ModelRegistry` lookup miss. |
| `ErrProviderNotSupported` | Profile exists but has no `ProviderModels` entry for the requested provider. |
| `ErrDuplicateModelProfile` | `ModelRegistry.Register` called twice for the same canonical ID. |
| `ErrInvalidModelProfile` | `ModelProfile.Validate()` failed. |

### Context cancellation

`context.Canceled` and `context.DeadlineExceeded` propagate through
all calls **unwrapped** — match them directly:

```go
if errors.Is(err, context.Canceled) {
    // ...
}
```

This is intentional: cancellation is a caller-side concern, distinct
from any provider-side timeout.

## Retry policy

The library applies a `RetryPolicy` after each chat call. A policy is
a list of `RetryPredicate`s; the first one that says "retry" wins.

### Default policy

```go
endpoint.DefaultRetryPolicy()
```

Returns a policy with `MaxRetries=1` and one predicate:
`zai_empty_tool_call`. This handles the Z.ai sporadic failure mode
where a tool-call response arrives with empty/unparseable arguments.

`NewChatClient` applies this policy when `ChatClientConfig.RetryPolicy`
is nil. To opt out explicitly, pass a non-nil empty policy:

```go
endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "...",
    RetryPolicy: &endpoint.RetryPolicy{}, // MaxRetries=0, no predicates
})
```

### Custom predicates

Write a new predicate by implementing the `RetryPredicate` interface:

```go
type RetryPredicate interface {
    Name() string  // stable identifier for logs/metrics
    ShouldRetry(req ChatRequest, resp *ChatResponse, err error) (retry bool, reason string)
}
```

Predicates **must be pure** — no state, no I/O. They get a read-only
view of the request (`ChatRequest`), the response (may be nil if
`err != nil`), and the error (may be nil if the call succeeded).

Example: retry on 503 for OpenAI specifically:

```go
type openaiUnavailable struct{}

func (openaiUnavailable) Name() string { return "openai_503" }

func (openaiUnavailable) ShouldRetry(req endpoint.ChatRequest, resp *endpoint.ChatResponse, err error) (bool, string) {
    if req.Provider != "openai" {
        return false, ""
    }
    if errors.Is(err, endpoint.ErrServiceUnavailable) {
        return true, "openai 503"
    }
    return false, ""
}

policy := &endpoint.RetryPolicy{
    MaxRetries: 2,
    Predicates: []endpoint.RetryPredicate{openaiUnavailable{}},
}

client, _ := endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL: "https://api.openai.com",
    APIKey:      os.Getenv("OPENAI_API_KEY"),
    RetryPolicy: policy,
})
```

### Backoff

The retry loop does **not** sleep between attempts — retries fire
immediately. The default policy's single retry handles a sporadic
Z.ai response-parsing failure where an immediate retry usually wins;
it is not designed for rate-limit or 5xx backoff.

If you need backoff (exponential, jitter, etc.) for rate limits or
transient 5xx failures, wrap the chat call yourself rather than
extending `RetryPolicy`. The policy surface is deliberately narrow.

### Observing retries

`RetryPolicy.OnRetry` is invoked once per retry decision with a
`RetryEvent`:

```go
policy := &endpoint.RetryPolicy{
    MaxRetries: 1,
    Predicates: []endpoint.RetryPredicate{openaiUnavailable{}},
    OnRetry: func(e endpoint.RetryEvent) {
        log.Printf("retry attempt=%d predicate=%s reason=%q latency=%s",
            e.Attempt, e.PredicateName, e.Reason, e.LatencyDelta)
    },
}
```

`OnRetry` fires AFTER the predicate has decided to retry but BEFORE
the next attempt. It MUST NOT block — emit metrics asynchronously if
you need to do anything slow.

### Streaming retries

`ChatStream` retries **only before the first chunk arrives**. Once the
callback has been invoked with content bytes, the stream is committed
and predicates do not re-fire. This avoids re-delivering partial
streams to the caller's callback.

### What's NOT retried

The library does not retry image or video generation calls. Image
calls are synchronous and any 5xx is forwarded to the caller; video
jobs are asynchronous, and the `WaitVideo` poll loop handles transient
poll failures internally without invoking `RetryPolicy`.

If you need retry behavior on image/video, wrap the call yourself.
