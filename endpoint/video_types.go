package endpoint

import (
	"context"
	"io"
	"time"
)

// VideoOptions configures a video generation request. Caller-facing
// surface; adapters translate to their provider-native wire shape.
// The vocabulary is borrowed from OpenRouter's pre-normalized cross-
// provider video schema so that adapter is near-passthrough.
type VideoOptions struct {
	// Model is the provider-specific model identifier. Required.
	// Z.ai examples: "vidu2-image", "vidu2-start-end", "vidu2-reference",
	//   "viduq1-text", "cogvideox-3".
	// OpenAI: "sora-2", "sora-2-pro".
	// OpenRouter: "openai/sora-2-pro", "google/veo-3.1", etc.
	Model string

	// Prompt is the text description. Required for text-to-video models;
	// optional for image-to-video / reference models when ImageURL is set.
	Prompt string

	// Size is the requested output resolution as "WIDTHxHEIGHT".
	// Allowed values are model-specific.
	Size string

	// Duration is the requested video length in seconds. Allowed values
	// are model-specific (Z.ai vidu2: 4; OpenAI sora-2: 16 or 20; etc.).
	// Zero means "use provider default".
	Duration int

	// AspectRatio is e.g. "16:9", "9:16", "1:1". Provider-specific
	// support; many models infer it from Size and ignore this field.
	AspectRatio string

	// ImageURL is one or more reference/start-frame image URLs (or
	// data: URIs with base64 payload). The semantic role depends on
	// model: first-frame for image-to-video; first+last for start-end
	// models; subject reference for vidu2-reference. Provider adapters
	// are responsible for validating count constraints (e.g. vidu2-
	// reference allows 1-3; viduq1-image allows exactly 1).
	ImageURL []string

	// MovementAmplitude is one of "auto", "small", "medium", "large".
	// Z.ai-vidu-family-specific; ignored by other providers.
	MovementAmplitude string

	// WithAudio requests synthesized audio (background music, sfx).
	// Provider-specific support.
	WithAudio bool

	// User is an end-user identifier for abuse-prevention surfacing.
	// Field-name translation per adapter mirrors the chat-side rule
	// (Z.ai uses user_id; OpenAI uses user; OpenRouter passes through
	// per their schema).
	User string

	// PollOptions overrides the wait-loop tunables when calling
	// WaitVideo. Defaults applied if zero.
	PollOptions PollOptions

	// ExtraBody fields are merged into the outbound submit JSON body
	// before send, with override-wins on key conflict.
	ExtraBody map[string]any

	// OutputDir, when non-empty, causes GenerateVideo (and the
	// standalone DownloadVideo helper) to write each completed
	// video to disk under this directory. Streams via the same
	// OpenContent path callers can use manually — bytes are NOT
	// eagerly buffered in memory. The result's LocalPath field is
	// populated with the absolute on-disk path.
	//
	// Filename pattern: {prompt-slug}-v{N}.{ext} for N=1, batch
	// not currently exercised by any provider. Default extension
	// is "mp4" for OpenAI/OpenRouter; Z.ai-returned URLs typically
	// have explicit extensions so the URL extension wins.
	OutputDir string

	// CreateOutputDir, when true, causes OutputDir (and parents)
	// to be created if missing. When false (default), a missing
	// OutputDir returns an error wrapping fs.ErrNotExist.
	CreateOutputDir bool

	// OnProgress, when non-nil, receives ProgressEvent updates
	// during polling (ProgressPolling) and download
	// (ProgressDownloading, then a final ProgressComplete).
	OnProgress ProgressFn `json:"-"`
}

// VideoJob represents the state of a single asynchronous video-
// generation job, returned by SubmitVideo, PollVideo, and WaitVideo.
type VideoJob struct {
	// ID is the provider-assigned task identifier — opaque to callers,
	// used to address subsequent poll calls.
	ID string

	// Status is the unified job state (see JobStatus).
	Status JobStatus

	// Progress is an integer 0-100 percentage when the provider
	// reports it, -1 when not available (e.g. Z.ai does not expose
	// a numeric progress field).
	Progress int

	// Created is the unix timestamp of the job's submission per the
	// provider, or zero when not reported.
	Created int64

	// Model is the resolved provider-specific model name.
	Model string

	// Videos carries the generated video(s). Populated only when
	// Status == JobStatusCompleted; empty otherwise.
	Videos []GeneratedVideo

	// Error is the upstream error message when Status ==
	// JobStatusFailed. Empty in non-failed states.
	Error string
}

// GeneratedVideo represents one video result. URL is populated when
// the provider embeds a download URL in the poll response (Z.ai,
// OpenRouter); empty for OpenAI which exposes content via a separate
// streaming endpoint. OpenContent is ALWAYS populated for completed
// jobs and provides a uniform retrieval API regardless of provider.
type GeneratedVideo struct {
	// URL is the provider's CDN URL when one is embedded in the poll
	// response. Empty for providers that stream content from a
	// separate endpoint (OpenAI). Always check OpenContent for
	// uniform access.
	URL string

	// OpenContent returns an io.ReadCloser streaming the video bytes.
	// For URL-bearing providers (Z.ai, OpenRouter) it issues an
	// authenticated HTTP-GET on the URL. For OpenAI it issues a GET
	// on /v1/videos/{id}/content. Caller MUST Close() the returned
	// reader.
	OpenContent func(ctx context.Context) (io.ReadCloser, error)

	// LocalPath is the absolute on-disk path of the file written
	// when VideoOptions.OutputDir was set (via GenerateVideo or
	// DownloadVideo). Empty otherwise.
	LocalPath string
}

// PollOptions configures the WaitVideo loop's pacing.
type PollOptions struct {
	// Interval is the time between successive poll attempts.
	// Default: 10 * time.Second (per OpenAI's documented guidance).
	Interval time.Duration

	// MaxWait is the maximum total time to wait before giving up
	// with ErrPollTimeout. Default: 15 * time.Minute.
	MaxWait time.Duration
}

func (o *PollOptions) applyDefaults() {
	if o.Interval <= 0 {
		o.Interval = 10 * time.Second
	}
	if o.MaxWait <= 0 {
		o.MaxWait = 15 * time.Minute
	}
}
