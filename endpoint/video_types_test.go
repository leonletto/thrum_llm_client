package endpoint

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestVideoOptions_ZeroValue(t *testing.T) {
	var o VideoOptions
	if o.Duration != 0 || o.WithAudio || o.Model != "" {
		t.Errorf("zero VideoOptions has non-zero fields: %+v", o)
	}
}

func TestVideoJob_Terminal(t *testing.T) {
	if !(&VideoJob{Status: JobStatusCompleted}).Status.IsTerminal() {
		t.Error("Completed must be terminal")
	}
	if (&VideoJob{Status: JobStatusInProgress}).Status.IsTerminal() {
		t.Error("InProgress must not be terminal")
	}
}

func TestPollOptions_Defaults(t *testing.T) {
	o := PollOptions{}
	o.applyDefaults()
	if o.Interval == 0 || o.MaxWait == 0 {
		t.Errorf("defaults not applied: %+v", o)
	}
	if o.Interval > o.MaxWait {
		t.Errorf("Interval %v > MaxWait %v after defaults", o.Interval, o.MaxWait)
	}
}

func TestGeneratedVideo_OpenContentInvocation(t *testing.T) {
	called := false
	gv := GeneratedVideo{
		URL: "https://cdn.example/v.mp4",
		OpenContent: func(_ context.Context) (io.ReadCloser, error) {
			called = true
			return io.NopCloser(strings.NewReader("MP4FAKE")), nil
		},
	}
	rc, err := gv.OpenContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if !called || string(body) != "MP4FAKE" {
		t.Errorf("called=%v body=%q", called, body)
	}
}

func TestPollOptions_AppliesCustomDurations(t *testing.T) {
	o := PollOptions{Interval: 1 * time.Second, MaxWait: 5 * time.Second}
	o.applyDefaults()
	if o.Interval != 1*time.Second || o.MaxWait != 5*time.Second {
		t.Errorf("custom values overwritten: %+v", o)
	}
}
