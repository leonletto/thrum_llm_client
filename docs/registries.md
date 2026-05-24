# Registries

The library ships with two optional registries that let you resolve
**canonical** identifiers to **provider-specific** ones at call time:

- **`ProviderRegistry`** — maps a provider name (`"openai"`,
  `"openrouter-prod"`, etc.) to the endpoint URL, API key env var,
  and capability set.
- **`ModelRegistry`** — maps a canonical model ID (`"glm-5.1"`,
  `"qwen3-30b-instruct"`) to per-provider model strings, default
  parameters, and per-model `ReasoningMode` policy.

Both load from YAML. Registries are **opt-in** — calls work without
them; the registries just let you avoid hardcoding provider-specific
strings throughout your call sites.

## Provider registry

### YAML format

```yaml
defaults:
  capabilities:
    chat: true
    embedding: false
    vision: false
    thinking: false
    streaming: true
    generate: false

providers:
  - name: openai-prod
    type: openai
    endpoint: https://api.openai.com
    api_key_env: OPENAI_API_KEY
    capabilities:
      embedding: true

  - name: zai-prod
    type: zai
    endpoint: https://api.z.ai
    api_key_env: ZAI_API_KEY
    capabilities:
      thinking: true

  - name: openrouter-prod
    type: openai  # OpenAI-compatible adapter
    endpoint: https://openrouter.ai/api
    api_key_env: OPENROUTER_API_KEY
    chat_path: /v1/chat/completions
    extra_headers:
      HTTP-Referer: https://your-app.example.com
      X-Title: Your App
    capabilities:
      embedding: true
```

The `defaults.capabilities` block is applied to every provider; each
provider's `capabilities` block overrides defaults per-field. The
`type` field selects the adapter (`openai`, `anthropic`, `zai`,
`ollama`); the `endpoint` host is otherwise the source of
auto-detection.

### Loading and using

```go
configs, err := endpoint.LoadProviders("config/providers.yaml")
if err != nil {
    log.Fatal(err)
}
providers := endpoint.NewProviderRegistry(configs)

// Resolve a client by provider name. EndpointURL, Provider, APIKey,
// ChatPath, and ExtraHeaders are all populated from the registry entry.
client, err := endpoint.NewChatClient(endpoint.ChatClientConfig{
    ProviderName:     "openrouter-prod",
    ProviderRegistry: providers,
})
```

The `APIKey` is read from the env var named in `api_key_env`. If the
env var is empty, the constructor returns `ErrProviderNotConfigured`.

## Model registry

### YAML format

```yaml
defaults:
  max_context_tokens: 128000
  max_output_tokens: 8000
  default_temperature: 0.7
  supports_streaming: true
  supports_embedding: false
  supports_reasoning: false
  supports_vision: false

models:
  - canonical_id: glm-5.1
    display_name: GLM 5.1
    supports_reasoning: true
    reasoning_mode: off  # explicit, but the default
    provider_models:
      zai: glm-5.1

  - canonical_id: qwen3-30b-instruct
    display_name: Qwen3 30B Instruct
    max_context_tokens: 32768
    provider_models:
      ollama: qwen3:30b-a3b-instruct
      vllm: Qwen/Qwen3-30B-A3B-Instruct

  - canonical_id: text-embed-small
    display_name: Small Embed Model
    supports_streaming: false
    supports_embedding: true
    max_output_tokens: 0
    provider_models:
      openai: text-embedding-3-small
      ollama: nomic-embed-text
```

`defaults` is merged into every entry per-field. `provider_models`
maps the **adapter type** (the `type` field from the provider config,
e.g. `"zai"`, `"openai"`, `"ollama"`) to the provider-specific model
identifier.

### Loading and using

```go
profiles, err := endpoint.LoadModels("config/models.yaml")
if err != nil {
    log.Fatal(err)
}
models := endpoint.NewEmptyModelRegistry()
for _, p := range profiles {
    if err := models.Register(p); err != nil {
        log.Fatal(err)
    }
}

client, _ := endpoint.NewChatClient(endpoint.ChatClientConfig{
    EndpointURL:   "https://api.z.ai",
    APIKey:        os.Getenv("ZAI_API_KEY"),
    ModelRegistry: models,
})

// Pass the canonical ID; the client resolves to the provider-specific
// model string via the registry.
resp, err := client.Chat(ctx, "glm-5.1",
    []endpoint.ChatMessage{{Role: "user", Content: "Hi"}}, nil)
// Wire model becomes "glm-5.1" (Z.ai's name), and the Z.ai adapter
// emits {"thinking":{"type":"disabled"}} per the registry's
// reasoning_mode policy.
```

When the registry is configured but the model name passed to `Chat`
is **not** a canonical ID, the library falls back to passing the
string through unchanged. This lets you mix canonical and
provider-specific model names in the same client.

### Reasoning mode

`ReasoningMode` is per-model, set in the YAML as
`reasoning_mode: off | auto | on`. See [providers.md](providers.md#reasoning-mode)
for what each value emits on the wire. Currently consumed only by the
Z.ai adapter; other adapters ignore it.

### Loading a single registry from one YAML file

If your registry is small and you don't want to call
`Register` in a loop:

```go
models := endpoint.NewEmptyModelRegistry()
if err := models.LoadFromFile("config/models.yaml"); err != nil {
    log.Fatal(err)
}
```

`LoadFromFile` validates duplicates and rejects invalid profiles
(missing `canonical_id`, empty `provider_models`, etc.).

## Combining the two registries

```go
providers, _ := endpoint.LoadProviders("config/providers.yaml")
profiles, _  := endpoint.LoadModels("config/models.yaml")

provReg := endpoint.NewProviderRegistry(providers)
modReg  := endpoint.NewEmptyModelRegistry()
for _, p := range profiles {
    modReg.Register(p)
}

client, _ := endpoint.NewChatClient(endpoint.ChatClientConfig{
    ProviderName:     "zai-prod",
    ProviderRegistry: provReg,
    ModelRegistry:    modReg,
})

resp, _ := client.Chat(ctx, "glm-5.1", msgs, nil)
// Provider, endpoint, key, headers, model name, and reasoning mode
// all resolved from the two registries.
```

This is the recommended pattern for any deployment touching more than
one provider — call sites refer to canonical names only.
