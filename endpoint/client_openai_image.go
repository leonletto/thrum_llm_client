package endpoint

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIImagePath is the OpenAI image-generations endpoint path.
const OpenAIImagePath = "/v1/images/generations"

type imageOpenAIClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newImageOpenAIClient(baseURL, apiKey string, extraHeaders map[string]string) *imageOpenAIClient {
	return &imageOpenAIClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *imageOpenAIClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *imageOpenAIClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *imageOpenAIClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *imageOpenAIClient) HTTPClient() *http.Client            { return c.httpClient }
func (c *imageOpenAIClient) ProviderName() string                { return "openai" }

type openaiImageRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Size    string `json:"size,omitempty"`
	Quality string `json:"quality,omitempty"`
	N       int    `json:"n,omitempty"`
	User    string `json:"user,omitempty"`
}

type openaiImageResponse struct {
	Created int64             `json:"created"`
	Data    []openaiImageData `json:"data"`
	Usage   *openaiImageUsage `json:"usage,omitempty"`
}

type openaiImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type openaiImageUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		ImageTokens int `json:"image_tokens"`
		TextTokens  int `json:"text_tokens"`
	} `json:"input_tokens_details"`
}

func (c *imageOpenAIClient) GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error) {
	req := openaiImageRequest{
		Model:   opts.Model,
		Prompt:  opts.Prompt,
		Size:    opts.Size,
		Quality: opts.Quality,
		N:       opts.N,
		User:    opts.User,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openai image: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("openai image: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("openai image: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+OpenAIImagePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai image: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai image: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "GenerateImage", respBody)
	}

	var parsed openaiImageResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("openai image: decode: %w", err)
	}

	out := &ImageResult{Created: parsed.Created}
	for _, d := range parsed.Data {
		img := GeneratedImage{URL: d.URL, RevisedPrompt: d.RevisedPrompt}
		if d.B64JSON != "" {
			decoded, err := base64.StdEncoding.DecodeString(d.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("openai image: decode b64_json: %w", err)
			}
			img.Bytes = decoded
		}
		out.Images = append(out.Images, img)
	}
	if parsed.Usage != nil {
		out.Usage = &ImageUsage{
			InputTokens:      parsed.Usage.InputTokens,
			OutputTokens:     parsed.Usage.OutputTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
			InputImageTokens: parsed.Usage.InputTokensDetails.ImageTokens,
			InputTextTokens:  parsed.Usage.InputTokensDetails.TextTokens,
		}
	}
	return out, nil
}

func (c *imageOpenAIClient) handleError(resp *http.Response, op string, body []byte) error {
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
