package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const OpenAIVideoSubmitPath = "/v1/videos"

type videoOpenAIClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newVideoOpenAIClient(baseURL, apiKey string, extraHeaders map[string]string) *videoOpenAIClient {
	return &videoOpenAIClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *videoOpenAIClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *videoOpenAIClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *videoOpenAIClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *videoOpenAIClient) ProviderName() string                { return "openai" }

type openaiVideoSubmitRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Size    string `json:"size,omitempty"`
	Seconds string `json:"seconds,omitempty"`
	User    string `json:"user,omitempty"`
}

type openaiVideoJob struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	CreatedAt int64  `json:"created_at"`
	Status    string `json:"status"`
	Model     string `json:"model"`
	// Progress is *int (not int) so we can disambiguate "field
	// absent" (nil → translate to -1) from "0% complete" (zero
	// pointer → keep as 0). The unified VideoJob.Progress contract
	// uses -1 for "not reported"; passing 0 through unconditionally
	// would lie about progress for the queued-but-not-started case.
	Progress *int   `json:"progress,omitempty"`
	Seconds  string `json:"seconds"`
	Size     string `json:"size"`
}

func openaiMapStatus(s string) JobStatus {
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

func (c *videoOpenAIClient) SubmitVideo(ctx context.Context, opts VideoOptions) (*VideoJob, error) {
	req := openaiVideoSubmitRequest{
		Model:  opts.Model,
		Prompt: opts.Prompt,
		Size:   opts.Size,
		User:   opts.User,
	}
	if opts.Duration > 0 {
		req.Seconds = strconv.Itoa(opts.Duration)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openai video submit: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("openai video submit: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("openai video submit: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+OpenAIVideoSubmitPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai video submit: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai video submit: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "SubmitVideo", respBody)
	}

	return c.toJob(respBody)
}

func (c *videoOpenAIClient) PollVideo(ctx context.Context, jobID string) (*VideoJob, error) {
	endpoint := c.baseURL + OpenAIVideoSubmitPath + "/" + jobID
	httpReq, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("openai video poll: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai video poll: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "PollVideo", respBody)
	}

	return c.toJob(respBody)
}

// toJob unmarshals an OpenAI video object and lifts it to a VideoJob.
// On Completed status, attaches a single GeneratedVideo whose
// OpenContent fetches /v1/videos/{id}/content.
func (c *videoOpenAIClient) toJob(body []byte) (*VideoJob, error) {
	var v openaiVideoJob
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("openai video: decode: %w", err)
	}
	progress := -1
	if v.Progress != nil {
		progress = *v.Progress
	}
	job := &VideoJob{
		ID:       v.ID,
		Status:   openaiMapStatus(v.Status),
		Progress: progress,
		Created:  v.CreatedAt,
		Model:    v.Model,
	}
	if job.Status == JobStatusCompleted && job.ID != "" {
		job.Videos = []GeneratedVideo{{
			URL:         "",
			OpenContent: c.openVideoContent(job.ID),
		}}
	}
	return job, nil
}

func (c *videoOpenAIClient) openVideoContent(id string) func(context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		endpoint := c.baseURL + OpenAIVideoSubmitPath + "/" + id + "/content"
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		applyExtraHeaders(req, c.extraHeaders)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("openai video content: status %d: %s", resp.StatusCode, body)
		}
		return resp.Body, nil
	}
}

func (c *videoOpenAIClient) handleError(resp *http.Response, op string, body []byte) error {
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
			return NewEndpointError(c.baseURL, "openai", op, fmt.Errorf("status %d: %s", resp.StatusCode, apiMsg))
		}
		return NewEndpointError(c.baseURL, "openai", op, fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
	}
	if apiMsg != "" {
		return NewEndpointError(c.baseURL, "openai", op, fmt.Errorf("%w: %s", sent, apiMsg))
	}
	return NewEndpointError(c.baseURL, "openai", op, sent)
}
