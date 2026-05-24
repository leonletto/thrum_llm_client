package endpoint

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAIVideo_Submit_QueuedJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"video_abc","object":"video","created_at":1700000000,"status":"queued","model":"sora-2-pro","seconds":"16","size":"1280x720"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	job, err := client.SubmitVideo(context.Background(), VideoOptions{
		Model: "sora-2-pro", Prompt: "A cat surfing", Size: "1280x720", Duration: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "video_abc" || job.Status != JobStatusQueued {
		t.Errorf("got %+v", job)
	}
	if job.Created != 1700000000 {
		t.Errorf("created = %d", job.Created)
	}
}

func TestOpenAIVideo_Poll_InProgressWithProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/videos/video_abc") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"video_abc","object":"video","status":"in_progress","model":"sora-2-pro","progress":33,"seconds":"16","size":"1280x720"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "video_abc")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusInProgress {
		t.Errorf("status = %v", job.Status)
	}
	if job.Progress != 33 {
		t.Errorf("progress = %d", job.Progress)
	}
}

func TestOpenAIVideo_Poll_CompletedHasOpenContent(t *testing.T) {
	contentCalls := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/content"):
			contentCalls.Add(1)
			_, _ = w.Write([]byte("MP4FAKE-openai"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"video_abc","object":"video","status":"completed","model":"sora-2-pro","seconds":"16","size":"1280x720"}`))
		}
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "video_abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Videos) != 1 {
		t.Fatalf("expected 1 video; got %d", len(job.Videos))
	}
	if job.Videos[0].URL != "" {
		t.Error("OpenAI URL must be empty (content via /content endpoint)")
	}
	rc, err := job.Videos[0].OpenContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "MP4FAKE-openai" {
		t.Errorf("body = %q", body)
	}
	if contentCalls.Load() != 1 {
		t.Errorf("/content not called")
	}
}

func TestOpenAIVideo_Poll_TerminalFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"video","status":"failed"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusFailed {
		t.Errorf("got %v", job.Status)
	}
}

func TestOpenAIVideo_ErrorSentinels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "openai", APIKey: "test"})
	_, err := client.SubmitVideo(context.Background(), VideoOptions{Model: "m", Prompt: "p"})
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("got %v; want ErrRateLimited", err)
	}
}
