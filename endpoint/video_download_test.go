package endpoint

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadVideo_SingleVideo(t *testing.T) {
	mp4 := []byte("FAKE-MP4-PAYLOAD-1234567890")
	dir := t.TempDir()
	job := &VideoJob{
		Status: JobStatusCompleted,
		Videos: []GeneratedVideo{{
			URL: "https://cdn.example.com/v.mp4",
			OpenContent: func(ctx context.Context) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(mp4)), nil
			},
		}},
	}
	err := DownloadVideo(context.Background(), job, VideoOptions{
		Prompt:    "a dancing robot",
		OutputDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	got := job.Videos[0].LocalPath
	if !strings.HasSuffix(got, "a-dancing-robot-v1.mp4") {
		t.Errorf("LocalPath = %q; want suffix a-dancing-robot-v1.mp4", got)
	}
	disk, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqual(disk, mp4) {
		t.Errorf("file bytes don't match payload")
	}
}

func TestDownloadVideo_MissingDirNoCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-such-dir")
	job := &VideoJob{
		Status: JobStatusCompleted,
		Videos: []GeneratedVideo{{
			URL:         "https://example.com/v.mp4",
			OpenContent: func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader([]byte("x"))), nil },
		}},
	}
	err := DownloadVideo(context.Background(), job, VideoOptions{
		Prompt:    "x",
		OutputDir: dir,
	})
	if err == nil {
		t.Fatal("expected fs.ErrNotExist error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("want errors.Is fs.ErrNotExist; got %v", err)
	}
}

func TestDownloadVideo_NotTerminalIsNoop(t *testing.T) {
	dir := t.TempDir()
	job := &VideoJob{
		Status: JobStatusInProgress,
		Videos: nil,
	}
	if err := DownloadVideo(context.Background(), job, VideoOptions{
		Prompt:    "x",
		OutputDir: dir,
	}); err != nil {
		t.Fatalf("unexpected error on non-terminal job: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected 0 files in dir; got %d", len(entries))
	}
}

func TestDownloadVideo_OpenContentError(t *testing.T) {
	dir := t.TempDir()
	job := &VideoJob{
		Status: JobStatusCompleted,
		Videos: []GeneratedVideo{{
			URL: "https://x",
			OpenContent: func(context.Context) (io.ReadCloser, error) {
				return nil, errors.New("network down")
			},
		}},
	}
	err := DownloadVideo(context.Background(), job, VideoOptions{
		Prompt: "x", OutputDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Errorf("expected wrapped 'network down' error; got %v", err)
	}
}

func TestDownloadVideo_RealHTTPViaOpenContent(t *testing.T) {
	mp4 := bytes.Repeat([]byte("MP4!"), 300*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1228800")
		w.WriteHeader(200)
		_, _ = w.Write(mp4)
	}))
	defer srv.Close()

	dir := t.TempDir()
	client := srv.Client()
	url := srv.URL + "/v.mp4"
	job := &VideoJob{
		Status: JobStatusCompleted,
		Videos: []GeneratedVideo{{
			URL: url,
			OpenContent: func(ctx context.Context) (io.ReadCloser, error) {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				resp, err := client.Do(req)
				if err != nil {
					return nil, err
				}
				return resp.Body, nil
			},
		}},
	}

	var progressSeen int
	if err := DownloadVideo(context.Background(), job, VideoOptions{
		Prompt: "stream me", OutputDir: dir,
		OnProgress: func(e ProgressEvent) { progressSeen++ },
	}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasSuffix(job.Videos[0].LocalPath, ".mp4") {
		t.Errorf("LocalPath = %q; want .mp4 suffix", job.Videos[0].LocalPath)
	}
	if progressSeen == 0 {
		t.Errorf("expected at least one progress event for ~1.2 MB stream")
	}
}
