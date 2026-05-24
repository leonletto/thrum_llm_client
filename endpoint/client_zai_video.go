package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ZaiVideoSubmitPath = "/api/paas/v4/videos/generations"
	ZaiVideoPollPath   = "/api/paas/v4/async-result"
)

type videoZaiClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newVideoZaiClient(baseURL, apiKey string, extraHeaders map[string]string) *videoZaiClient {
	return &videoZaiClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *videoZaiClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *videoZaiClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *videoZaiClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *videoZaiClient) ProviderName() string                { return "zai" }

// zaiVideoSubmitRequest mirrors the Z.ai vidu/cogvideox wire body.
// Field semantics follow https://docs.z.ai/api-reference/video/generate-video.
type zaiVideoSubmitRequest struct {
	Model             string `json:"model"`
	Prompt            string `json:"prompt,omitempty"`
	ImageURL          any    `json:"image_url,omitempty"`
	Duration          int    `json:"duration,omitempty"`
	Size              string `json:"size,omitempty"`
	AspectRatio       string `json:"aspect_ratio,omitempty"`
	MovementAmplitude string `json:"movement_amplitude,omitempty"`
	WithAudio         bool   `json:"with_audio,omitempty"`
	UserID            string `json:"user_id,omitempty"`
}

// zaiVideoSubmitResponse handles BOTH documented variants — the
// docs spec a top-level {id, task_status, ...}; the reference impl
// also handles a wrapped {data:{id}, task_status} shape. omitempty
// + nil-pointer guard lets us accept either.
type zaiVideoSubmitResponse struct {
	Data       *zaiVideoSubmitData `json:"data,omitempty"`
	ID         string              `json:"id,omitempty"`
	Model      string              `json:"model,omitempty"`
	RequestID  string              `json:"request_id,omitempty"`
	TaskStatus string              `json:"task_status,omitempty"`
}
type zaiVideoSubmitData struct {
	ID string `json:"id"`
}

type zaiVideoPollResponse struct {
	TaskStatus  string         `json:"task_status"`
	VideoResult []zaiVideoData `json:"video_result,omitempty"`
	Error       string         `json:"error,omitempty"`
}
type zaiVideoData struct {
	URL string `json:"url"`
}

func zaiMapStatus(s string) JobStatus {
	switch s {
	case "PROCESSING":
		return JobStatusInProgress
	case "SUCCESS":
		return JobStatusCompleted
	case "FAIL":
		return JobStatusFailed
	}
	return JobStatusUnknown
}

func (c *videoZaiClient) SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error) {
	req := zaiVideoSubmitRequest{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		Duration:          opts.Duration,
		Size:              opts.Size,
		AspectRatio:       opts.AspectRatio,
		MovementAmplitude: opts.MovementAmplitude,
		WithAudio:         opts.WithAudio,
		UserID:            opts.User,
	}
	if len(opts.ImageURL) == 1 && (strings.Contains(opts.Model, "vidu2-image") || strings.Contains(opts.Model, "viduq1-image")) {
		req.ImageURL = opts.ImageURL[0]
	} else if len(opts.ImageURL) > 0 {
		req.ImageURL = opts.ImageURL
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("zai video submit: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("zai video submit: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("zai video submit: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+ZaiVideoSubmitPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zai video submit: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zai video submit: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "SubmitVideo", respBody)
	}

	var parsed zaiVideoSubmitResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("zai video submit: decode: %w", err)
	}

	id := parsed.ID
	if id == "" && parsed.Data != nil {
		id = parsed.Data.ID
	}
	if id == "" {
		return nil, fmt.Errorf("zai video submit: response missing id")
	}

	return &VideoJob{
		ID:       id,
		Status:   zaiMapStatus(parsed.TaskStatus),
		Progress: -1,
		Model:    parsed.Model,
	}, nil
}

func (c *videoZaiClient) PollVideo(ctx context.Context, jobID string) (*VideoJob, error) {
	endpoint := c.baseURL + ZaiVideoPollPath + "/" + url.PathEscape(jobID)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("zai video poll: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zai video poll: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "PollVideo", respBody)
	}

	var parsed zaiVideoPollResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("zai video poll: decode: %w", err)
	}

	job := &VideoJob{
		ID:       jobID,
		Status:   zaiMapStatus(parsed.TaskStatus),
		Progress: -1,
		Error:    parsed.Error,
	}
	if job.Status == JobStatusCompleted {
		for _, v := range parsed.VideoResult {
			vid := GeneratedVideo{URL: v.URL}
			vid.OpenContent = c.openURLContent(v.URL)
			job.Videos = append(job.Videos, vid)
		}
	}
	return job, nil
}

// openURLContent returns a closure that HTTP-GETs the given URL with
// the adapter's HTTP client. Caller MUST Close().
//
// IMPORTANT: We do NOT forward c.apiKey or c.extraHeaders to CDN
// URLs. Z.ai/OpenRouter video URLs are pre-signed and the CDN host
// is third-party; sending caller-supplied headers (e.g. an
// HTTP-Referer or proxy auth) to that host could leak internal
// routing information. The download happens with a clean request.
func (c *videoZaiClient) openURLContent(u string) func(context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("zai video content: status %d: %s", resp.StatusCode, body)
		}
		return resp.Body, nil
	}
}

func (c *videoZaiClient) handleError(resp *http.Response, op string, body []byte) error {
	var apiMsg string
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		apiMsg = errResp.Error.Message
	}
	sent := httpStatusToSentinel(resp.StatusCode)
	if sent == nil {
		if apiMsg != "" {
			return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, "zai", op, fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, "zai", op, sent)
}
