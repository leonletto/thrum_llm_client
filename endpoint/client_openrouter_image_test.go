package endpoint

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouterImage_Success(t *testing.T) {
	wantBytes := []byte("PNG\x89fakedata-or-somesuch")
	b64 := base64.StdEncoding.EncodeToString(wantBytes)
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-or-1",
			"created": 1700000000,
			"choices": [{
				"index": 0,
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": [
						{"type":"text","text":"Here is your image:"},
						{"type":"image_url","image_url":{"url":"data:image/png;base64,` + b64 + `"}}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()

	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{
		Model: "google/gemini-2.5-flash-image", Prompt: "kitten", Size: "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("expected 1 image; got %d", len(res.Images))
	}
	if string(res.Images[0].Bytes) != string(wantBytes) {
		t.Errorf("decoded bytes mismatch")
	}

	var body map[string]any
	_ = json.Unmarshal(captured, &body)
	mods, _ := body["modalities"].([]any)
	if len(mods) == 0 || mods[0] != "image" {
		t.Errorf("modalities wire field wrong: %v", body["modalities"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message; got %v", msgs)
	}
}

func TestOpenRouterImage_HTTPSURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message":{"role":"assistant","content":[
				{"type":"image_url","image_url":{"url":"https://cdn.openrouter/img.png"}}
			]}}]
		}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Images[0].URL != "https://cdn.openrouter/img.png" {
		t.Errorf("URL = %q", res.Images[0].URL)
	}
	if len(res.Images[0].Bytes) != 0 {
		t.Error("Bytes must be empty when response carries an https URL")
	}
}

func TestExtractContentBlocks(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantLen int
		wantErr bool
	}{
		{"nil raw", "", 0, false},
		{"null", "null", 0, false},
		{"empty string raw", "\"\"", 0, false},
		{"text string", "\"hello world\"", 0, false},
		{"empty array", "[]", 0, false},
		{"single image block", `[{"type":"image_url","image_url":{"url":"https://x/y.png"}}]`, 1, false},
		{"text+image array", `[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://x/y.png"}}]`, 2, false},
		{"unexpected shape", "42", 0, true},
		{"malformed array", `[{"type":`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks, err := extractContentBlocks(json.RawMessage(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (blocks=%+v)", blocks)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(blocks) != tc.wantLen {
				t.Errorf("len=%d; want %d", len(blocks), tc.wantLen)
			}
		})
	}
}

func TestOpenRouterImage_NullContentWithImagesSibling(t *testing.T) {
	wantBytes := []byte("PNG\x89fake-gemini-shape")
	b64 := base64.StdEncoding.EncodeToString(wantBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-or-gemini",
			"created": 1700000001,
			"choices": [{
				"index": 0,
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": null,
					"images": [
						{"type":"image_url","image_url":{"url":"data:image/png;base64,` + b64 + `"}}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{Model: "google/gemini-2.5-flash-image", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("len(Images)=%d; want 1", len(res.Images))
	}
	if !bytes.Equal(res.Images[0].Bytes, wantBytes) {
		t.Errorf("decoded bytes mismatch")
	}
}

func TestOpenRouterImage_StringContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-or-text",
			"created": 1700000002,
			"choices": [{
				"index": 0,
				"finish_reason": "stop",
				"message": {"role":"assistant","content":"only text, no images"}
			}]
		}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatalf("expected nil error on string-shaped content, got %v", err)
	}
	if len(res.Images) != 0 {
		t.Errorf("len(Images)=%d; want 0 (text-only response)", len(res.Images))
	}
}

func TestOpenRouterImage_ContentAndImagesMerged(t *testing.T) {
	wantA := []byte("PNG-A")
	wantB := []byte("PNG-B")
	b64A := base64.StdEncoding.EncodeToString(wantA)
	b64B := base64.StdEncoding.EncodeToString(wantB)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-or-merged",
			"created": 1700000003,
			"choices": [{
				"index": 0,
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": [
						{"type":"image_url","image_url":{"url":"data:image/png;base64,` + b64A + `"}}
					],
					"images": [
						{"type":"image_url","image_url":{"url":"data:image/png;base64,` + b64B + `"}}
					]
				}
			}]
		}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	res, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 2 {
		t.Fatalf("len(Images)=%d; want 2 (content+images merged)", len(res.Images))
	}
	if !bytes.Equal(res.Images[0].Bytes, wantA) || !bytes.Equal(res.Images[1].Bytes, wantB) {
		t.Errorf("merge order wrong; want [A,B]")
	}
}

func TestOpenRouterImage_ErrorSentinels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	_, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("got %v; want ErrForbidden", err)
	}
}
