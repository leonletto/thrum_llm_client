package endpoint

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterVideo_Submit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"or_vid_1","status":"queued","model":"openai/sora-2-pro"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	job, err := client.SubmitVideo(context.Background(), VideoOptions{
		Model: "openai/sora-2-pro", Prompt: "A cat", Duration: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "or_vid_1" || job.Status != JobStatusQueued {
		t.Errorf("got %+v", job)
	}
}

func TestOpenRouterVideo_Poll_Completed_OpenContentFetchesURL(t *testing.T) {
	var cdnAuth string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("MP4FAKE-or"))
	}))
	defer cdn.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/videos/or_vid_1") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"or_vid_1","status":"completed","videos":[{"url":"` + cdn.URL + `/v.mp4"}]}`))
	}))
	defer api.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: api.URL, Provider: "openrouter", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "or_vid_1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusCompleted || len(job.Videos) != 1 {
		t.Fatalf("got %+v", job)
	}
	rc, err := job.Videos[0].OpenContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "MP4FAKE-or" {
		t.Errorf("body = %q", body)
	}
	// CDN host differs from configured endpoint host — Bearer token
	// MUST NOT be forwarded to a third-party host.
	if cdnAuth != "" {
		t.Errorf("third-party CDN received Authorization header %q; want none", cdnAuth)
	}
}

func TestOpenRouterVideo_SameHost(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "https://openrouter.ai/api/v1/videos/abc/content", "https://openrouter.ai/api", true},
		{"case-insensitive", "https://OpenRouter.AI/x", "https://openrouter.ai/y", true},
		{"different host", "https://cdn.example.com/v.mp4", "https://openrouter.ai/api", false},
		{"empty a", "", "https://openrouter.ai/api", false},
		{"empty b", "https://openrouter.ai/x", "", false},
		{"malformed a", "://nope", "https://openrouter.ai/api", false},
		{"malformed b", "https://openrouter.ai/x", "://nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameHost(tc.a, tc.b); got != tc.want {
				t.Errorf("sameHost(%q,%q) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestOpenRouterVideo_PollUnsignedURLs(t *testing.T) {
	cdnHits := 0
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/content") {
			cdnHits++
			capturedAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("MP4FAKE-or-unsigned"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{"id":"or_vid_unsigned","status":"completed","unsigned_urls":["http://` + r.Host + `/v1/videos/or_vid_unsigned/content?index=0"]}`
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test-token"})
	job, err := client.PollVideo(context.Background(), "or_vid_unsigned")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusCompleted || len(job.Videos) != 1 {
		t.Fatalf("got status=%v videos=%+v", job.Status, job.Videos)
	}
	if job.Videos[0].OpenContent == nil {
		t.Fatal("OpenContent must be wired")
	}
	rc, err := job.Videos[0].OpenContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "MP4FAKE-or-unsigned" {
		t.Errorf("body = %q", body)
	}
	if cdnHits != 1 {
		t.Errorf("content endpoint hit %d times; want 1", cdnHits)
	}
	if capturedAuth != "Bearer test-token" {
		t.Errorf("Authorization on same-host fetch = %q; want %q", capturedAuth, "Bearer test-token")
	}
}

func TestOpenRouterVideo_ErrorSentinels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":{"message":"unknown model"}}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openrouter", APIKey: "test"})
	_, err := client.SubmitVideo(context.Background(), VideoOptions{Model: "x/y", Prompt: "p"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}
