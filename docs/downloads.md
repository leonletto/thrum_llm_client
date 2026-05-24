# Downloads

Image and video generation both support an **opt-in** download to a
caller-supplied directory. Set `OutputDir` on `ImageOptions` or
`VideoOptions` and the library writes each artifact under that
directory with a predictable, versioned filename.

## Basic image download

```go
res, err := imgClient.GenerateImage(ctx, endpoint.ImageOptions{
    Model:     "cogview-4-250304",
    Prompt:    "a red cat at dusk",
    OutputDir: "/var/lib/myapp/images",
})

// res.Images[0].LocalPath -> "/var/lib/myapp/images/a-red-cat-at-dusk-v1.png"
```

The original `URL` (or `Bytes`) field on `GeneratedImage` is **also**
populated, so callers can still stream the content or share the URL.
`LocalPath` is added; nothing is removed.

## Basic video download

`UnifiedVideoClient.GenerateVideo` composes Submit + Wait + Download
in one call:

```go
job, err := vidClient.GenerateVideo(ctx, endpoint.VideoOptions{
    Model:     "sora-2-pro",
    Prompt:    "a cat surfing a wave",
    Size:      "1280x720",
    Duration:  16,
    OutputDir: "/var/lib/myapp/videos",
})

// job.Videos[0].LocalPath -> "/var/lib/myapp/videos/a-cat-surfing-a-wave-v1.mp4"
```

For callers using the three-phase `SubmitVideo` / `WaitVideo` API
directly, the standalone `endpoint.DownloadVideo(ctx, job, opts)`
helper applies the same logic to a completed `*VideoJob`:

```go
job, _ := vidClient.SubmitVideo(ctx, opts)
job, _ = vidClient.WaitVideo(ctx, job.ID, endpoint.PollOptions{})

opts.OutputDir = "/var/lib/myapp/videos"
err := endpoint.DownloadVideo(ctx, job, opts)
```

## Filename naming

| Case | Pattern | Example |
| --- | --- | --- |
| Single image | `{prompt-slug}-v{N}.{ext}` | `a-red-cat-v1.png` |
| Image batch (N>1) | `{prompt-slug}-v{N}-{idx}.{ext}` | `a-red-cat-v2-3.png` |
| Single video | `{prompt-slug}-v{N}.{ext}` | `a-cat-walking-v1.mp4` |

The version `N` is the smallest integer that does not collide with an
existing file for the same slug and extension in `OutputDir`. The
library scans the directory before each call and picks the next free
number — there are no overwrites.

For image batches, all files in one batch share the same `N`; the
`idx` (0-based) disambiguates within the batch. A subsequent batch
with the same prompt picks `N+1`.

**Slug derivation:** lowercase, ASCII alphanumerics + hyphens. The
slug is bounded in length, so very long prompts are truncated cleanly.
Two prompts that differ only in punctuation or case produce the same
slug and share the version counter.

**Extension:**

- **Image**: derived from the response (`png` from base64 PNG bytes,
  `jpg` from URLs ending in `.jpg`, etc.).
- **Video**: defaults to `mp4` for OpenAI/OpenRouter; Z.ai's CDN URLs
  carry explicit extensions and those win.

## Directory existence

`OutputDir` must exist by default. If it does not, the call returns
an error wrapping `fs.ErrNotExist`:

```go
if errors.Is(err, fs.ErrNotExist) {
    // OutputDir doesn't exist and CreateOutputDir was false
}
```

To auto-create the directory (and parents), set `CreateOutputDir: true`:

```go
endpoint.ImageOptions{
    // ...
    OutputDir:       "/var/lib/myapp/images",
    CreateOutputDir: true,  // calls os.MkdirAll
}
```

## Partial-write safety

If the download fails partway through (network drop, context
cancellation, write error), the partial file is **unlinked** before
the error is returned. Directory listings never show half-finished
files.

## Progress callbacks

Set `OnProgress` to receive `ProgressEvent` notifications during the
download (and, for video, during the async poll).

```go
endpoint.ImageOptions{
    // ...
    OutputDir: "/tmp/images",
    OnProgress: func(e endpoint.ProgressEvent) {
        switch e.Phase {
        case endpoint.ProgressDownloading:
            log.Printf("downloading %d / %d bytes", e.BytesDone, e.BytesTotal)
        case endpoint.ProgressComplete:
            log.Printf("done: %d bytes", e.BytesDone)
        }
    },
}
```

### Phases

| Phase | When it fires | Fields populated |
| --- | --- | --- |
| `ProgressPolling` | After each video poll iteration (never for image — sync) | `Status` (latest `JobStatus`), `Percent` (provider integer, -1 if unknown) |
| `ProgressDownloading` | Periodically during disk writes | `BytesDone` (cumulative), `BytesTotal` (Content-Length, -1 if unknown), `Percent` (ratio when total known) |
| `ProgressComplete` | Once per successful generation, after writes flush | `BytesDone` (final file size) |

Download events are throttled at the lower of **256 KB** or **200 ms**
— the callback fires whenever the next event would cross either
threshold. This keeps progress UIs responsive without flooding the
callback on fast networks.

### Callback contract

- Called from the goroutine doing the work. Callers are responsible
  for any cross-goroutine synchronization.
- **Must not block** on a long operation — use a buffered channel and
  consume from another goroutine if you need to do anything slow.
- Has no return value. Cancellation flows through the `ctx` passed
  to the generation call.

## Stream-through (no disk write)

If you want to forward bytes elsewhere without buffering through disk,
leave `OutputDir` empty and use `GeneratedVideo.OpenContent` directly:

```go
job, _ := vidClient.GenerateVideo(ctx, opts) // OutputDir empty

rc, err := job.Videos[0].OpenContent(ctx)
defer rc.Close()

io.Copy(myUploadStream, rc)
```

`OpenContent` is uniform across providers — Z.ai/OpenRouter HTTP-GET
the embedded URL with the configured Bearer token (only when the URL
host matches the configured endpoint host); OpenAI streams from
`/v1/videos/{id}/content`.

`OpenContent` remains populated **even when** `OutputDir` is set, so a
single call site can choose between path-based and stream-based
access.
