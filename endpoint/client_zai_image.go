package endpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ZaiImagePath is the chat-completions image-generations path on Z.ai.
const ZaiImagePath = "/api/paas/v4/images/generations"

type imageZaiClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newImageZaiClient(baseURL, apiKey string, extraHeaders map[string]string) *imageZaiClient {
	return &imageZaiClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *imageZaiClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *imageZaiClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *imageZaiClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *imageZaiClient) HTTPClient() *http.Client            { return c.httpClient }

func (c *imageZaiClient) ProviderName() string { return "zai" }

type zaiImageRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Size    string `json:"size,omitempty"`
	Quality string `json:"quality,omitempty"`
	UserID  string `json:"user_id,omitempty"`
}

type zaiImageResponse struct {
	Created       int64              `json:"created"`
	Data          []zaiImageData     `json:"data"`
	ContentFilter []zaiContentFilter `json:"content_filter,omitempty"`
}

type zaiImageData struct {
	URL string `json:"url"`
}

type zaiContentFilter struct {
	Role  string `json:"role"`
	Level int    `json:"level"`
}

func (c *imageZaiClient) GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error) {
	if opts.N > 1 {
		return nil, fmt.Errorf("zai image: N>1 not supported (got N=%d)", opts.N)
	}
	req := zaiImageRequest{
		Model:   opts.Model,
		Prompt:  opts.Prompt,
		Size:    opts.Size,
		Quality: opts.Quality,
		UserID:  opts.User,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("zai image: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("zai image: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("zai image: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+ZaiImagePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("zai image: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("zai image: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "GenerateImage", respBody)
	}

	var parsed zaiImageResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("zai image: decode: %w", err)
	}

	out := &ImageResult{Created: parsed.Created}
	for _, d := range parsed.Data {
		out.Images = append(out.Images, GeneratedImage{URL: d.URL})
	}
	for _, cf := range parsed.ContentFilter {
		out.ContentFilter = append(out.ContentFilter, ContentFilterEntry{
			Role: cf.Role, Level: cf.Level,
		})
	}
	return out, nil
}

func (c *imageZaiClient) handleError(resp *http.Response, op string, body []byte) error {
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
