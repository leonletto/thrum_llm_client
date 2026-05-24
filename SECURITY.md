# Security Policy

## Reporting a vulnerability

If you believe you've found a security issue in this library, please **do
not** open a public GitHub issue.

Instead, use GitHub's private vulnerability reporting:

1. Go to <https://github.com/leonletto/thrum_llm_client/security/advisories>
2. Click "Report a vulnerability"

Or email the maintainer directly with subject line `[security] thrum_llm_client`:

- **Leon Letto** — see commit author email in `git log`.

Please include:

- A description of the vulnerability and its impact.
- A minimal reproduction (ideally a Go test against `httptest`).
- The affected version(s) — typically the current `main` and the most
  recent tagged release.

You can expect an acknowledgment within 7 days. A fix or mitigation will
be coordinated privately before public disclosure.

## Scope

In scope for security reports:

- **Credential leakage** — any path where an `APIKey` or `ExtraHeaders`
  value would leave the configured `EndpointURL` host. The library's
  download paths are designed to forward Bearer tokens **only** when the
  target URL host matches the configured endpoint host; a violation of
  that invariant is in scope.
- **Request injection** — any code path where caller-supplied input
  (model name, prompt, image URL, etc.) could trigger unintended HTTP
  requests to an unexpected host.
- **TLS handling** — anything that allows downgrading or disabling TLS
  verification on credentialed paths.
- **YAML loading** — the `ProviderRegistry` / `ModelRegistry` loaders
  use `gopkg.in/yaml.v3` and load from caller-supplied paths. Any path-
  traversal or arbitrary-read issue rooted in the loader is in scope.

Out of scope:

- Vulnerabilities in upstream provider APIs (OpenAI, Anthropic, Z.ai,
  OpenRouter, Ollama). Report those to the relevant provider.
- Vulnerabilities in `gopkg.in/yaml.v3` itself. Report those to the
  upstream project; this library will track via `go.mod` bumps.
- Performance issues, missing features, or non-security bugs — those go
  in [regular issues](https://github.com/leonletto/thrum_llm_client/issues).

## Supported versions

Only the most recent minor version receives security fixes. The library
is pre-1.0 and the API may evolve; pin a specific version in your
`go.mod` and review the [CHANGELOG](CHANGELOG.md) before upgrading.
