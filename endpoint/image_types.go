package endpoint

// ImageOptions configures an image generation request. Caller-facing
// surface; adapters translate to their provider-native wire shape
// (see CHANGELOG and per-provider adapter files for the mapping).
type ImageOptions struct {
	// Model is the provider-specific model identifier. Required.
	// Examples: "cogView-4-250304" (Z.ai), "gpt-image-1" (OpenAI),
	// "google/gemini-2.5-flash-image" (OpenRouter).
	Model string

	// Prompt is the text description of the desired image. Required.
	Prompt string

	// Size is the requested image dimensions as "WIDTHxHEIGHT".
	// Allowed values are model-specific — consult provider docs.
	// Empty string means "use provider default for this model".
	Size string

	// Quality is a provider-specific quality hint. Empty string
	// means "use provider default". Z.ai accepts "hd"/"standard";
	// OpenAI accepts "auto"/"low"/"medium"/"high".
	Quality string

	// N is the number of images to generate. 0 means "use provider
	// default" (typically 1). Z.ai's adapter rejects N > 1.
	N int

	// User is an end-user identifier for abuse-prevention surfacing.
	// Z.ai's adapter renames this to user_id on the wire; OpenAI's
	// adapter uses it as user; OpenRouter ignores it.
	User string

	// ExtraBody fields are merged into the outbound JSON request
	// body before send, with override-wins on key conflict. Mirrors
	// the chat client's ExtraBody contract.
	ExtraBody map[string]any

	// OutputDir, when non-empty, causes GenerateImage to write each
	// returned image to disk under this directory. The library
	// downloads from GeneratedImage.URL (HTTP-GET) or decodes
	// GeneratedImage.Bytes (base64-payload-already-in-memory) and
	// streams to a versioned file. The result's LocalPath field is
	// populated with the absolute on-disk path; the existing URL
	// and Bytes fields remain unchanged.
	//
	// Filenames follow {prompt-slug}-v{N}.{ext} for N=1 results,
	// {prompt-slug}-v{N}-{idx}.{ext} for batches. Slug is derived
	// from ImageOptions.Prompt; version N is one greater than the
	// max already present in OutputDir for the same slug+ext.
	OutputDir string

	// CreateOutputDir, when true, causes GenerateImage to create
	// OutputDir (and parents) if missing. When false (default), a
	// missing OutputDir returns an error wrapping fs.ErrNotExist.
	// Only meaningful when OutputDir is non-empty.
	CreateOutputDir bool

	// OnProgress, when non-nil, receives ProgressEvent
	// notifications during download. Image generation is sync, so
	// only ProgressDownloading and ProgressComplete phases fire
	// (never ProgressPolling). The callback is invoked from the
	// caller's goroutine; callers are responsible for any cross-
	// goroutine synchronization.
	OnProgress ProgressFn `json:"-"`
}

// ImageResult is the response from a successful image generation
// request. Provider-specific extensions are surfaced as optional
// typed fields; check len(...)>0 / non-nil before consuming.
type ImageResult struct {
	// Created is the unix timestamp of the upstream request as
	// reported by the provider. Zero when the provider does not
	// report it.
	Created int64

	// Images carries the generated image(s). At least one entry
	// when the request succeeded.
	Images []GeneratedImage

	// ContentFilter is Z.ai's content-safety metadata. Empty for
	// other providers.
	ContentFilter []ContentFilterEntry

	// Usage is OpenAI's token-usage report. Nil for other providers.
	Usage *ImageUsage
}

// GeneratedImage represents one image in the response. Either URL
// or Bytes is populated, depending on the provider's response shape
// — callers should consult both. RevisedPrompt is OpenAI dall-e-3
// specific; empty for other providers.
type GeneratedImage struct {
	URL   string
	Bytes []byte

	// LocalPath is the absolute on-disk path of the file written
	// when ImageOptions.OutputDir was set. Empty when OutputDir
	// was not set.
	LocalPath string

	RevisedPrompt string
}

// ContentFilterEntry is one entry in Z.ai's content_filter array.
// Severity Level: 0=most severe, 3=least severe. Role values are
// "assistant", "user", or "history".
type ContentFilterEntry struct {
	Role  string
	Level int
}

// ImageUsage is OpenAI's token-usage report for image generation.
type ImageUsage struct {
	InputTokens      int
	OutputTokens     int
	TotalTokens      int
	InputImageTokens int
	InputTextTokens  int
}
