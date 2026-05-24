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

const OpenRouterVideoSubmitPath = "/v1/videos"

type videoOpenRouterClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newVideoOpenRouterClient(baseURL, apiKey string, extraHeaders map[string]string) *videoOpenRouterClient {
	return &videoOpenRouterClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *videoOpenRouterClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *videoOpenRouterClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *videoOpenRouterClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *videoOpenRouterClient) ProviderName() string                { return "openrouter" }

// orVideoSubmitRequest uses OpenRouter's normalized video schema.
// Field names mirror their announced cross-provider vocabulary.
type orVideoSubmitRequest struct {
	Model             string   `json:"model"`
	Prompt            string   `json:"prompt,omitempty"`
	ImageURL          []string `json:"image_url,omitempty"`
	Duration          int      `json:"duration,omitempty"`
	Size              string   `json:"size,omitempty"`
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	MovementAmplitude string   `json:"movement_amplitude,omitempty"`
	WithAudio         bool     `json:"with_audio,omitempty"`
}

type orVideoJob struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Model  string        `json:"model,omitempty"`
	Videos []orVideoData `json:"videos,omitempty"`
	// UnsignedURLs is the alternative result shape OpenRouter returns
	// for some providers on terminal "completed" status (e.g. veo-3.1-lite
	// returns a single entry pointing at /api/v1/videos/{id}/content?index=N).
	// When present and Videos is empty, callers should materialize Videos
	// from these URLs.
	UnsignedURLs []string `json:"unsigned_urls,omitempty"`
	Error        string   `json:"error,omitempty"`
}
type orVideoData struct {
	URL string `json:"url"`
}

// orMapStatus mirrors OpenAI's enum (OpenRouter aligns with it per
// their announcement framing; treat unknown values as Unknown).
func orMapStatus(s string) JobStatus {
	switch s {
	case "queued":
		return JobStatusQueued
	case "in_progress":
		return JobStatusInProgress
	case "completed":
		return JobStatusCompleted
	case "failed":
		return JobStatusFailed
	}
	return JobStatusUnknown
}

func (c *videoOpenRouterClient) SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error) {
	req := orVideoSubmitRequest{
		Model:             opts.Model,
		Prompt:            opts.Prompt,
		ImageURL:          opts.ImageURL,
		Duration:          opts.Duration,
		Size:              opts.Size,
		AspectRatio:       opts.AspectRatio,
		MovementAmplitude: opts.MovementAmplitude,
		WithAudio:         opts.WithAudio,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter video submit: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter video submit: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter video submit: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+OpenRouterVideoSubmitPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter video submit: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter video submit: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, c.handleError(resp, "SubmitVideo", respBody)
	}
	return c.toJob(respBody)
}

func (c *videoOpenRouterClient) PollVideo(ctx context.Context, jobID string) (*VideoJob, error) {
	endpoint := c.baseURL + OpenRouterVideoSubmitPath + "/" + jobID
	httpReq, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("openrouter video poll: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter video poll: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, c.handleError(resp, "PollVideo", respBody)
	}
	return c.toJob(respBody)
}

func (c *videoOpenRouterClient) toJob(body []byte) (*VideoJob, error) {
	var v orVideoJob
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("openrouter video: decode: %w", err)
	}
	job := &VideoJob{
		ID:       v.ID,
		Status:   orMapStatus(v.Status),
		Progress: -1,
		Model:    v.Model,
		Error:    v.Error,
	}
	if job.Status == JobStatusCompleted {
		for _, vd := range v.Videos {
			vid := GeneratedVideo{URL: vd.URL, OpenContent: c.openURLContent(vd.URL)}
			job.Videos = append(job.Videos, vid)
		}
		// Some OR providers (e.g. veo-3.1-lite) return unsigned_urls
		// instead of a videos array. Materialize Videos from them when
		// the structured array is absent.
		if len(job.Videos) == 0 {
			for _, u := range v.UnsignedURLs {
				vid := GeneratedVideo{URL: u, OpenContent: c.openURLContent(u)}
				job.Videos = append(job.Videos, vid)
			}
		}
	}
	return job, nil
}

// sameHost reports whether a and b parse to the same hostname
// (case-insensitive). Returns false if either URL is malformed or
// host-less. Used to decide whether to forward the caller's Bearer
// token to a result-content URL — we forward only when the URL is
// on the same host as the configured OR endpoint.
func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil || ua.Host == "" {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil || ub.Host == "" {
		return false
	}
	return strings.EqualFold(ua.Host, ub.Host)
}

// openURLContent fetches the video bytes from u. Forwards the
// caller's Bearer token only when u is on the same host as the
// configured OR endpoint (e.g. openrouter.ai's /api/v1/videos/{id}/content
// returns 401 without it). Third-party CDN hosts never receive the
// credential.
func (c *videoOpenRouterClient) openURLContent(u string) func(context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		// Defensive: drop any pre-existing Authorization, then conditionally set ours.
		req.Header.Del("Authorization")
		if sameHost(u, c.baseURL) && c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("openrouter video content: status %d: %s", resp.StatusCode, body)
		}
		return resp.Body, nil
	}
}

func (c *videoOpenRouterClient) handleError(resp *http.Response, op string, body []byte) error {
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
			return NewEndpointError(c.baseURL, "openrouter", op, fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, "openrouter", op, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, "openrouter", op, fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, "openrouter", op, sent)
}
