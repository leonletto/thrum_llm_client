# Contributing

Thanks for considering a contribution. This is a small, focused Go library
with a deliberate one-runtime-dependency rule and a single public package
(`endpoint`). The guidance below is meant to keep the contribution loop
fast and predictable.

## Prerequisites

- **Go 1.25 or newer** — required by `go.mod`. The CI uses `go-version-file:
  go.mod` so your local Go version should match.
- **Make** — for the standard quality gates.

No Node, no UI build, no codegen. The whole library builds in seconds.

## Setting up

```bash
git clone https://github.com/leonletto/thrum_llm_client.git
cd thrum_llm_client
go mod download
make quality
```

`make quality` runs vet + tests (with the race detector) + build. If it
passes, your environment is ready.

## Quality gates

The CI runs four checks (`.github/workflows/ci.yml`). Run them locally
before pushing:

| Command | What it does |
| --- | --- |
| `gofmt -s -l .` | Should print nothing. Format with `gofmt -s -w .` if it doesn't. |
| `make vet` | `go vet ./...` |
| `make test` | Hermetic unit tests under `./endpoint/`. |
| `make test-race` | Same tests with the race detector. |
| `make build` | `go build ./...` |
| `make quality` | All of the above. |

CI is mandatory; if it fails, the PR doesn't merge. There are no other
linters in CI — keep code clean by hand.

## Live-API smoke suite (optional)

The `tests/e2e/` directory contains a build-tag-gated suite that calls real
provider APIs. It is **not** run by `go test ./...` or by CI.

```bash
# Requires a .env file at the repo root with ZAI_API_KEY and OPENROUTER_API_KEY.
make e2e
```

Tests skip cleanly when the relevant key is unset. See
[docs/testing.md](docs/testing.md) for details.

## The one-dependency rule

The library ships exactly one runtime dependency: `gopkg.in/yaml.v3`. PRs
that add a new dependency need a clear justification. In particular:

- **`.env` parsing** is intentionally in-tree (e2e helper) rather than
  pulling in `godotenv`.
- **HTTP retries / backoff** use stdlib + the in-tree `RetryPolicy` rather
  than `cenkalti/backoff`, `hashicorp/go-retryablehttp`, etc.
- **JSON** uses `encoding/json` — no `jsoniter`, `go-json`, etc.

If you genuinely need a new dependency, open an issue first to discuss the
tradeoff before writing the PR.

## Adding a provider adapter

The codebase is organized as `UnifiedChatClient` / `UnifiedImageClient` /
`UnifiedVideoClient` dispatching to per-provider adapter files named
`client_<provider>[_<modality>].go`. To add a new provider:

1. **Pick the modalities.** Most provider additions are chat-only. Image
   and video require their own adapters because the wire shapes differ
   substantially between providers.
2. **Write the adapter.** Implement `ChatClientAdapter`, and/or
   `ImageClientAdapter`, and/or `VideoClientAdapter`. Look at
   `client_anthropic.go` (chat-only, simple) or `client_openrouter_image.go`
   (chat-modality-image, three response-shape branches) as references.
3. **Wire detection.** Add a host pattern to `detectProvider`
   (`chat_client.go`), `createImageAdapter` (`image_client.go`), and/or
   `createVideoAdapter` (`video_client.go`).
4. **Return typed errors.** Map HTTP status codes via
   `httpStatusToSentinel` and wrap in `*EndpointError`. Tests should
   verify with `errors.Is`. See [docs/errors-and-retries.md](docs/errors-and-retries.md).
5. **Test against a stub server.** Use `net/http/httptest` — all existing
   adapters do. Don't pull in mocking frameworks.

## Pull request conventions

- **One logical change per PR.** Mix-and-match diffs (style + new feature
  + dependency bump) are hard to review and to revert.
- **[Conventional Commits](https://www.conventionalcommits.org/) prefixes:**
  `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `ci:`,
  `style:`, `perf:`. Breaking changes use `!`: `feat!: rename Embed`.
- **Update CHANGELOG.md** under `## [Unreleased]` for user-visible
  changes (new APIs, behavior changes, bug fixes). Pure refactors and
  test-only PRs don't need a changelog entry.
- **Tests required for behavior changes.** Adapter changes should land
  with a test that pins the new behavior on a `httptest` stub.

## Releasing

The release process is automated. A maintainer:

1. Bumps the `## [Unreleased]` heading in `CHANGELOG.md` to the new version.
2. Commits and pushes to `main`.
3. Creates and pushes a `v0.Y.Z` annotated tag.

The `.github/workflows/release.yml` workflow runs on tag push: it verifies
tests, creates a GitHub Release with auto-generated notes, and primes
`proxy.golang.org` so `pkg.go.dev` indexes the new version within minutes.

Tags containing a `-` suffix (e.g. `v0.2.0-rc.1`) are published as
GitHub prereleases automatically.

## Reporting bugs

Open an issue with:

1. The provider, model, and modality.
2. The exact call (config and call site) that failed.
3. The error you saw, including `errors.Is` checks if you tried any.
4. The minimal repro — ideally a `httptest`-backed Go test that
   demonstrates the issue without your API key.

For security issues, see [SECURITY.md](SECURITY.md).
