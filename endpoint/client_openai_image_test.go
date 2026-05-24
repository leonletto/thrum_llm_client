package endpoint

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIImage_Success_URL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created": 1700000000,
			"data": [{"url":"https://cdn.openai.com/img/x.png", "revised_prompt":"a sunny cat"}],
			"usage": {"input_tokens":29, "output_tokens":1568, "total_tokens":1597, "input_tokens_details":{"image_tokens":0,"text_tokens":29}}
		}`))
	}))
	defer srv.Close()

	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{
		Model: "dall-e-3", Prompt: "kitten", Size: "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Images[0].URL != "https://cdn.openai.com/img/x.png" {
		t.Errorf("URL = %q", res.Images[0].URL)
	}
	if res.Images[0].RevisedPrompt != "a sunny cat" {
		t.Errorf("RevisedPrompt missing")
	}
	if res.Usage == nil || res.Usage.TotalTokens != 1597 {
		t.Errorf("Usage missing/wrong: %+v", res.Usage)
	}
}

func TestOpenAIImage_Success_B64(t *testing.T) {
	wantBytes := []byte("PNG\x89fakedata")
	b64 := base64.StdEncoding.EncodeToString(wantBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1700000000,"data":[{"b64_json":"` + b64 + `"}]}`))
	}))
	defer srv.Close()

	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{Model: "gpt-image-1", Prompt: "kitten"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Images[0].Bytes) != string(wantBytes) {
		t.Errorf("decoded bytes mismatch")
	}
	if res.Images[0].URL != "" {
		t.Errorf("URL must be empty when b64_json is the response shape")
	}
}

func TestOpenAIImage_WireBody_FieldNames(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":0,"data":[{"url":"x"}]}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	_, _ = client.GenerateImage(context.Background(), ImageOptions{
		Model: "gpt-image-1", Prompt: "p", User: "user-123", N: 2,
	})
	var body map[string]any
	_ = json.Unmarshal(captured, &body)
	if body["user"] != "user-123" {
		t.Errorf("user wire field = %v; want user-123", body["user"])
	}
	if _, hasUserID := body["user_id"]; hasUserID {
		t.Error("user_id must NOT appear in OpenAI wire body — that's Z.ai-specific")
	}
	if int(body["n"].(float64)) != 2 {
		t.Errorf("n wire field = %v; want 2", body["n"])
	}
}

func TestOpenAIImage_ErrorSentinels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	_, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("got %v; want ErrRateLimited", err)
	}
}
