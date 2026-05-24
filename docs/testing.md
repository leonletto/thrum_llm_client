# Testing

The library has two test surfaces:

1. **Unit tests** — hermetic, `httptest`-backed, run on every push.
2. **E2E smoke suite** — live API calls, build-tag-gated, opt-in.

## Unit tests

```bash
make test         # go test ./endpoint/ -v
make test-race    # go test -race ./endpoint/
make quality      # vet + test + race + build (the full CI gate)
```

All adapter behavior is verified against an in-process `httptest.Server`
that mimics the provider's wire shape. There is no mocking framework
and no network access. A green `make quality` means the four CI
checks (gofmt -s, vet, test-race, build) will pass.

To run a single test:

```bash
go test ./endpoint/ -run TestUnifiedChatClient_AutoDetectsAnthropic -v
```

To force a rerun even when source is unchanged (Go caches passing
tests):

```bash
make clean-cache  # go clean -testcache
make test
```

### What's covered

- Constructor validation (`EndpointURL` parsing, provider
  auto-detection, capability checks).
- Per-provider adapter wire-shape construction and response parsing.
- Streaming SSE / NDJSON parsing, including the 8 MB scanner ceiling
  for chunks carrying base64 images or long tool-call arguments.
- Tool-call delta accumulation across streaming chunks (Z.ai).
- Reasoning-mode resolver under concurrent access.
- `ProgressEvent` throttling and phase transitions.
- Filename versioning (single + batch, collision handling).
- `RetryPolicy` predicate execution, including stream-cutover
  behavior.
- HTTP status → sentinel error mapping for every adapter.
- YAML loading for both registries, including defaults-merge.

### What's NOT covered

- Live API behavior. See the e2e suite below.
- Semantic content quality (does the LLM say something sensible).
- Cross-version regression against actual provider deployments —
  the unit tests pin against `httptest` stubs of the wire shape, not
  against the real provider.

## E2E smoke suite

The `tests/e2e/` directory contains a `//go:build e2e`-gated test
suite that calls live provider APIs. Standard `go test ./...` does
**not** touch it; the CI does not run it. Use it for:

- Validating adapter changes against the real provider before tagging
  a release.
- Detecting provider-side wire-shape drift (the unit tests will keep
  passing if a provider quietly changes a field name).

```bash
# Requires a .env file at the repo root.
make e2e
```

`make e2e` runs:

```bash
go test -tags=e2e -v -timeout 15m ./tests/e2e/...
```

### Required environment

Create `.env` at the repo root with at least one of:

```
ZAI_API_KEY=...
OPENROUTER_API_KEY=...
```

The suite parses `.env` in-tree (no `godotenv` dependency). Tests
skip cleanly when the relevant key is unset:

```
--- SKIP: TestZaiChatLive (0.00s)
    zai_chat_test.go:42: skipping live test: ZAI_API_KEY not set
```

Add `OPENAI_API_KEY` if you also want to exercise OpenAI's image/video
paths.

### What the e2e suite verifies

The e2e suite is **structural only**. It asserts:

- The response populates expected fields (text content, image bytes
  or URL, video LocalPath).
- The expected `ProgressPhase` events fire in the expected order.
- Saved files exist on disk with the expected size floor and
  extension.
- Slug-derived filenames match the expected pattern.

It does **not** assert content quality, model output similarity, or
provider response timing.

### Output location

Each e2e test writes its artifacts under `testdata/` at the repo
root. This directory is gitignored so generated images and videos
don't pollute commits. Inspect them after a run:

```bash
ls testdata/
# a-cat-walking-across-a-desk-v1.mp4
# a-red-apple-on-a-wooden-table-v1.png
# ...
```

Delete the directory to start clean:

```bash
rm -rf testdata/
make e2e
```

## Adding tests

For new adapter features, write a unit test against `httptest` that
pins the wire-shape contract. The pattern across the codebase:

```go
func TestSomething(t *testing.T) {
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Assert request shape
        if r.Header.Get("Authorization") != "Bearer test-key" {
            t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
        }

        // Reply with the provider's response shape
        json.NewEncoder(w).Encode(map[string]any{
            "choices": []map[string]any{{
                "message": map[string]any{"role": "assistant", "content": "hi"},
            }},
        })
    }))
    defer ts.Close()

    client, _ := endpoint.NewChatClient(endpoint.ChatClientConfig{
        EndpointURL: ts.URL,
        Provider:    "openai",
        APIKey:      "test-key",
    })

    resp, err := client.Chat(context.Background(), "test-model",
        []endpoint.ChatMessage{{Role: "user", Content: "hi"}}, nil)
    if err != nil {
        t.Fatal(err)
    }
    if resp.Content != "hi" {
        t.Errorf("got content %q, want %q", resp.Content, "hi")
    }
}
```

For e2e additions, mirror the existing files in `tests/e2e/`. Use
`//go:build e2e` at the top and check for the required env var before
calling `client.Chat` / `GenerateImage` / etc. — `t.Skip` if missing.
