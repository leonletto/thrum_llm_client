//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/leonletto/thrum_llm_client/endpoint"
)

const openrouterImageModel = "google/gemini-2.5-flash-image"

func TestOpenRouterImage(t *testing.T) {
	apiKey := requireEnv(t, "OPENROUTER_API_KEY")
	outDir := outputDir(t, "openrouter", "image")
	prog, getPhases := progressRecorder()

	client, err := endpoint.NewImageClient(endpoint.ImageClientConfig{
		EndpointURL: openrouterEndpoint,
		APIKey:      apiKey,
	})
	if err != nil {
		t.Fatalf("NewImageClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const prompt = "a red apple on a wooden table"
	result, err := client.GenerateImage(ctx, endpoint.ImageOptions{
		Model:           openrouterImageModel,
		Prompt:          prompt,
		OutputDir:       outDir,
		CreateOutputDir: true,
		OnProgress:      prog,
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(result.Images) == 0 {
		t.Fatalf("GenerateImage returned 0 images")
	}
	first := result.Images[0]
	if first.LocalPath == "" {
		t.Fatalf("GenerateImage did not populate LocalPath")
	}
	assertDownloadedFile(t, first.LocalPath, 1024, ".png", ".jpg", ".jpeg", ".webp")
	assertSlugNaming(t, first.LocalPath, "a-red-apple-on-a-wooden-table")

	phases := getPhases()
	if !containsPhase(phases, endpoint.ProgressDownloading) {
		t.Fatalf("phases=%v; missing ProgressDownloading", phases)
	}
	if !containsPhase(phases, endpoint.ProgressComplete) {
		t.Fatalf("phases=%v; missing ProgressComplete", phases)
	}
	if containsPhase(phases, endpoint.ProgressPolling) {
		t.Fatalf("phases=%v; image flow must not emit ProgressPolling", phases)
	}
}
