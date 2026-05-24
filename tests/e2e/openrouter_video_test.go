//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/leonletto/thrum_llm_client/endpoint"
)

const openrouterVideoModel = "google/veo-3.1-lite"

func TestOpenRouterVideo(t *testing.T) {
	apiKey := requireEnv(t, "OPENROUTER_API_KEY")
	outDir := outputDir(t, "openrouter", "video")
	prog, getPhases := progressRecorder()

	client, err := endpoint.NewVideoClient(endpoint.VideoClientConfig{
		EndpointURL: openrouterEndpoint,
		APIKey:      apiKey,
	})
	if err != nil {
		t.Fatalf("NewVideoClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	const prompt = "a cat walking across a desk"
	job, err := client.GenerateVideo(ctx, endpoint.VideoOptions{
		Model:  openrouterVideoModel,
		Prompt: prompt,
		// 4s is the shortest veo-3.1-lite supports; minimizes per-test cost.
		Duration:        4,
		OutputDir:       outDir,
		CreateOutputDir: true,
		OnProgress:      prog,
		PollOptions:     endpoint.PollOptions{MaxWait: 5 * time.Minute},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}
	if job.Status != endpoint.JobStatusCompleted {
		t.Fatalf("GenerateVideo terminal Status=%v; want JobStatusCompleted (job.Error=%q)", job.Status, job.Error)
	}
	if len(job.Videos) == 0 {
		t.Fatalf("GenerateVideo returned 0 videos")
	}
	first := job.Videos[0]
	if first.LocalPath == "" {
		t.Fatalf("GenerateVideo did not populate LocalPath")
	}
	assertDownloadedFile(t, first.LocalPath, 10*1024, ".mp4")
	assertSlugNaming(t, first.LocalPath, "a-cat-walking-across-a-desk")

	phases := getPhases()
	if !containsPhase(phases, endpoint.ProgressPolling) {
		t.Fatalf("phases=%v; missing ProgressPolling", phases)
	}
	if !containsPhase(phases, endpoint.ProgressDownloading) {
		t.Fatalf("phases=%v; missing ProgressDownloading", phases)
	}
	if !containsPhase(phases, endpoint.ProgressComplete) {
		t.Fatalf("phases=%v; missing ProgressComplete", phases)
	}
}
