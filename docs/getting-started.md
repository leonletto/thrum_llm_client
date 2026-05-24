# Getting started

This guide walks through the first end-to-end call for each modality:
chat (with and without streaming), image generation, and video generation.

## Install

```bash
go get github.com/leonletto/thrum_llm_client@latest
```

Import the single package:

```go
import "github.com/leonletto/thrum_llm_client/endpoint"
```

The package is named `endpoint`. Most APIs are constructors
(`NewChatClient`, `NewImageClient`, `NewVideoClient`) followed by a
single method call (`Chat`, `GenerateImage`, `GenerateVideo`).

## Chat

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/leonletto/thrum_llm_client/endpoint"
)

func main() {
    client, err := endpoint.NewChatClient(endpoint.ChatClientConfig{
        EndpointURL: "https://api.openai.com",
        APIKey:      os.Getenv("OPENAI_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Chat(context.Background(), "gpt-4o",
        []endpoint.ChatMessage{
            {Role: "user", Content: "Hello"},
        }, nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

Provider is auto-detected from the URL host. To override, pass
`Provider: "openai"` (or `"anthropic"`, `"zai"`, `"ollama"`) in the
config.

## Streaming

`ChatStream` invokes a callback for each text chunk. It returns the
accumulated tool calls (empty for non-tool responses):

```go
_, err := client.ChatStream(ctx, "gpt-4o",
    []endpoint.ChatMessage{{Role: "user", Content: "Count to 5"}},
    nil,
    func(chunk string) error {
        fmt.Print(chunk)
        return nil
    })
```

Returning an error from the callback cancels the stream and is returned
from `ChatStream`.

## Embeddings

```go
vectors, err := client.Embed(ctx, "text-embedding-3-small",
    []string{"hello world", "good morning"})
// vectors is [][]float64, one slice per input.
```

Only providers that report embedding support will succeed; others
return `endpoint.ErrCapabilityNotSupported`.

## Image generation

```go
imgClient, err := endpoint.NewImageClient(endpoint.ImageClientConfig{
    EndpointURL: "https://api.z.ai",
    APIKey:      os.Getenv("ZAI_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}

result, err := imgClient.GenerateImage(ctx, endpoint.ImageOptions{
    Model:  "cogView-4-250304",
    Prompt: "A kitten on a sunny windowsill",
    Size:   "1024x1024",
})
if err != nil {
    log.Fatal(err)
}

url := result.Images[0].URL
fmt.Println("Image URL:", url)
```

URL lifetime varies by provider — Z.ai signed URLs are valid for ~30
days, OpenAI dall-e URLs for ~1 hour. To save a copy to disk
automatically, set `OutputDir`; see [downloads.md](downloads.md).

## Video generation

Video is async: submit, then poll until terminal. The
`GenerateVideo` convenience wrapper composes submit + wait + download:

```go
vidClient, err := endpoint.NewVideoClient(endpoint.VideoClientConfig{
    EndpointURL: "https://api.openai.com",
    APIKey:      os.Getenv("OPENAI_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}

job, err := vidClient.GenerateVideo(ctx, endpoint.VideoOptions{
    Model:     "sora-2-pro",
    Prompt:    "A cat surfing a wave at sunset",
    Size:      "1280x720",
    Duration:  16,
    OutputDir: "/tmp/videos",
})
if err != nil {
    log.Fatal(err)
}

if job.Status != endpoint.JobStatusCompleted {
    log.Fatalf("job ended in %s: %s", job.Status, job.Error)
}

fmt.Println("Saved to:", job.Videos[0].LocalPath)
```

If you need fine-grained control over the poll loop, call
`SubmitVideo` and `WaitVideo` separately:

```go
job, err := vidClient.SubmitVideo(ctx, opts)
// ... do other work ...
job, err = vidClient.WaitVideo(ctx, job.ID, endpoint.PollOptions{
    Interval: 10 * time.Second,
    MaxWait:  15 * time.Minute,
})

rc, err := job.Videos[0].OpenContent(ctx)
defer rc.Close()
io.Copy(file, rc)
```

`OpenContent` is uniform across providers — Z.ai and OpenRouter
return embedded CDN URLs; OpenAI streams from a separate
`/v1/videos/{id}/content` endpoint. The caller never branches on
provider.

## Next steps

- **[providers.md](providers.md)** — per-provider quirks, URL formats,
  capability matrix, and Z.ai reasoning-mode policy.
- **[errors-and-retries.md](errors-and-retries.md)** — typed sentinel
  errors and customizing `RetryPolicy`.
- **[downloads.md](downloads.md)** — automatic download-to-disk with
  progress callbacks and versioned filenames.
- **[registries.md](registries.md)** — YAML provider/model registries
  for canonical model resolution across providers.
- **[testing.md](testing.md)** — running the live-API smoke suite.
