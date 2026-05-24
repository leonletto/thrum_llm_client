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

// OpenRouterChatPath is OpenRouter's chat-completions endpoint;
// image generation flows through it via the modalities parameter.
const OpenRouterChatPath = "/v1/chat/completions"

type imageOpenRouterClient struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	extraBody    map[string]any
	extraHeaders map[string]string
}

func newImageOpenRouterClient(baseURL, apiKey string, extraHeaders map[string]string) *imageOpenRouterClient {
	return &imageOpenRouterClient{
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       apiKey,
		extraHeaders: extraHeaders,
	}
}

func (c *imageOpenRouterClient) setHTTPClient(h *http.Client)        { c.httpClient = h }
func (c *imageOpenRouterClient) setExtraBody(b map[string]any)       { c.extraBody = b }
func (c *imageOpenRouterClient) setExtraHeaders(h map[string]string) { c.extraHeaders = h }
func (c *imageOpenRouterClient) HTTPClient() *http.Client            { return c.httpClient }
func (c *imageOpenRouterClient) ProviderName() string                { return "openrouter" }

type openrouterImageRequest struct {
	Model       string                 `json:"model"`
	Messages    []openrouterMessage    `json:"messages"`
	Modalities  []string               `json:"modalities"`
	ImageConfig *openrouterImageConfig `json:"image_config,omitempty"`
}

type openrouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openrouterImageConfig struct {
	ImageSize string `json:"image_size,omitempty"`
}

type openrouterChatResponse struct {
	ID      string             `json:"id"`
	Created int64              `json:"created"`
	Choices []openrouterChoice `json:"choices"`
}

type openrouterChoice struct {
	Index        int               `json:"index"`
	FinishReason string            `json:"finish_reason"`
	Message      openrouterRespMsg `json:"message"`
}

type openrouterRespMsg struct {
	Role string `json:"role"`
	// Content is either a string (text-only response) or an array of
	// content blocks (multi-modal response). We decode it as RawMessage
	// and dispatch in extractContentBlocks.
	Content json.RawMessage `json:"content"`
	// Images is a sibling field returned by image-modality models
	// (e.g. google/gemini-2.5-flash-image). When present, content is
	// typically null and the image blocks live here instead.
	Images []openrouterContentBlock `json:"images"`
}

// extractContentBlocks returns the content blocks in raw, accepting
// either a JSON array (multi-modal shape) or a JSON string/null
// (text-only shape, which yields zero image blocks).
func extractContentBlocks(raw json.RawMessage) ([]openrouterContentBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	trim := bytes.TrimLeft(raw, " \t\r\n")
	if len(trim) == 0 {
		return nil, nil
	}
	switch trim[0] {
	case '[':
		var blocks []openrouterContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, err
		}
		return blocks, nil
	case '"':
		// text-only response: no image blocks
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected content shape: %s", string(raw))
	}
}

type openrouterContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

func (c *imageOpenRouterClient) GenerateImage(ctx context.Context, opts ImageOptions) (*ImageResult, error) {
	req := openrouterImageRequest{
		Model:      opts.Model,
		Messages:   []openrouterMessage{{Role: "user", Content: opts.Prompt}},
		Modalities: []string{"image", "text"},
	}
	if opts.Size != "" {
		req.ImageConfig = &openrouterImageConfig{ImageSize: opts.Size}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter image: marshal: %w", err)
	}
	body, err = mergeExtraBody(body, c.extraBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter image: %w", err)
	}
	body, err = mergeExtraBody(body, opts.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter image: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+OpenRouterChatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter image: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	applyExtraHeaders(httpReq, c.extraHeaders)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter image: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, c.handleError(resp, "GenerateImage", respBody)
	}

	var parsed openrouterChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("openrouter image: decode: %w", err)
	}

	out := &ImageResult{Created: parsed.Created}
	if len(parsed.Choices) == 0 {
		return out, nil
	}
	// Some image-modality models (e.g. google/gemini-2.5-flash-image)
	// return image blocks under message.images with message.content set
	// to null. Iterate both fields so either shape works.
	msg := parsed.Choices[0].Message
	contentBlocks, err := extractContentBlocks(msg.Content)
	if err != nil {
		return nil, fmt.Errorf("openrouter image: %w", err)
	}
	blocks := make([]openrouterContentBlock, 0, len(contentBlocks)+len(msg.Images))
	blocks = append(blocks, contentBlocks...)
	blocks = append(blocks, msg.Images...)
	for _, blk := range blocks {
		if blk.Type != "image_url" || blk.ImageURL == nil {
			continue
		}
		img := GeneratedImage{}
		url := blk.ImageURL.URL
		if strings.HasPrefix(url, "data:") {
			idx := strings.Index(url, ";base64,")
			if idx < 0 {
				return nil, fmt.Errorf("openrouter image: malformed data URI (missing ;base64,)")
			}
			b64 := url[idx+len(";base64,"):]
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return nil, fmt.Errorf("openrouter image: decode data URI: %w", err)
			}
			img.Bytes = decoded
		} else {
			img.URL = url
		}
		out.Images = append(out.Images, img)
	}
	return out, nil
}

func (c *imageOpenRouterClient) handleError(resp *http.Response, op string, body []byte) error {
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
