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

func TestZaiVideo_Submit_TopLevelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"zaijob-123","model":"vidu2-image","request_id":"rq-1","task_status":"PROCESSING"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	job, err := client.SubmitVideo(context.Background(), VideoOptions{
		Model: "vidu2-image", Prompt: "p", ImageURL: []string{"https://example/x.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "zaijob-123" || job.Status != JobStatusInProgress {
		t.Errorf("got %+v", job)
	}
}

func TestZaiVideo_Submit_WrappedID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"zaijob-456"},"task_status":"PROCESSING"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	job, err := client.SubmitVideo(context.Background(), VideoOptions{Model: "vidu2-image", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "zaijob-456" {
		t.Errorf("got ID %q; want zaijob-456", job.ID)
	}
}

func TestZaiVideo_Poll_TerminalSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/async-result/zaijob-123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_status":"SUCCESS","video_result":[{"url":"https://cdn.z.ai/v.mp4"}]}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "zaijob-123")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusCompleted {
		t.Errorf("status = %v; want Completed", job.Status)
	}
	if len(job.Videos) != 1 || job.Videos[0].URL != "https://cdn.z.ai/v.mp4" {
		t.Errorf("videos = %+v", job.Videos)
	}
	if job.Videos[0].OpenContent == nil {
		t.Error("OpenContent must be wired")
	}
}

func TestZaiVideo_Poll_TerminalFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_status":"FAIL","error":"content policy"}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusFailed || job.Error != "content policy" {
		t.Errorf("got %+v", job)
	}
}

func TestZaiVideo_OpenContent_FetchesURL(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("MP4FAKE-zai"))
	}))
	defer cdn.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_status":"SUCCESS","video_result":[{"url":"` + cdn.URL + `/v.mp4"}]}`))
	}))
	defer api.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: api.URL, Provider: "zai", APIKey: "test"})
	job, err := client.PollVideo(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rc, err := job.Videos[0].OpenContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "MP4FAKE-zai" {
		t.Errorf("body = %q", body)
	}
}

func TestZaiVideo_Wait_LoopsAcrossStatuses(t *testing.T) {
	calls := atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := calls.Add(1)
		switch {
		case n == 1:
			_, _ = w.Write([]byte(`{"id":"j","task_status":"PROCESSING"}`))
		case n == 2:
			_, _ = w.Write([]byte(`{"task_status":"PROCESSING"}`))
		default:
			_, _ = w.Write([]byte(`{"task_status":"SUCCESS","video_result":[{"url":"https://cdn/x.mp4"}]}`))
		}
	}))
	defer api.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: api.URL, Provider: "zai", APIKey: "test"})
	job, err := client.SubmitVideo(context.Background(), VideoOptions{Model: "vidu2-image", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	final, err := client.WaitVideo(context.Background(), job.ID, PollOptions{Interval: 5_000_000, MaxWait: 1_000_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != JobStatusCompleted {
		t.Errorf("status = %v", final.Status)
	}
}

func TestZaiVideo_ErrorSentinels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer srv.Close()
	client, _ := NewVideoClient(VideoClientConfig{EndpointURL: srv.URL, Provider: "zai", APIKey: "test"})
	_, err := client.SubmitVideo(context.Background(), VideoOptions{Model: "m", Prompt: "p"})
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Errorf("got %v; want ErrServiceUnavailable", err)
	}
}
