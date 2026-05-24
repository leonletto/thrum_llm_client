package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestZaiImage_Success(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created": 1700000000,
			"data": [{"url": "https://cdn.z.ai/img/abc.png"}],
			"content_filter": [{"role":"assistant","level":3}]
		}`))
	}))
	defer srv.Close()

	client, err := NewImageClient(ImageClientConfig{
		EndpointURL: srv.URL, Provider: "zai", APIKey: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := client.GenerateImage(context.Background(), ImageOptions{
		Model: "cogView-4-250304", Prompt: "kitten", Size: "1024x1024", Quality: "hd", User: "user-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 || res.Images[0].URL != "https://cdn.z.ai/img/abc.png" {
		t.Errorf("unexpected images: %+v", res.Images)
	}
	if res.Created != 1700000000 {
		t.Errorf("created = %d; want 1700000000", res.Created)
	}
	if len(res.ContentFilter) != 1 || res.ContentFilter[0].Level != 3 {
		t.Errorf("unexpected content_filter: %+v", res.ContentFilter)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("body unmarshal: %v (raw=%s)", err, captured)
	}
	if body["model"] != "cogView-4-250304" || body["prompt"] != "kitten" || body["user_id"] != "user-123" {
		t.Errorf("wire body wrong: %+v", body)
	}
	if _, hasUser := body["user"]; hasUser {
		t.Error("user must NOT be sent on Z.ai wire — only user_id")
	}
}

func TestZaiImage_RejectsNGreaterThanOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	_, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p", N: 2})
	if err == nil {
		t.Fatal("expected error for N>1")
	}
}

func TestZaiImage_ErrorSentinels(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"401", 401, ErrAuthenticationRequired},
		{"403", 403, ErrForbidden},
		{"404", 404, ErrNotFound},
		{"429", 429, ErrRateLimited},
		{"500", 500, ErrServerError},
		{"503", 503, ErrServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream rejected"}}`))
			}))
			defer srv.Close()
			client, _ := NewImageClient(ImageClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
			_, err := client.GenerateImage(context.Background(), ImageOptions{Model: "m", Prompt: "p"})
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v; want errors.Is == %v", err, tc.want)
			}
		})
	}
}
