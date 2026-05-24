package endpoint

import "testing"

func TestJobStatus_String(t *testing.T) {
	cases := map[JobStatus]string{
		JobStatusUnknown:    "unknown",
		JobStatusQueued:     "queued",
		JobStatusInProgress: "in_progress",
		JobStatusCompleted:  "completed",
		JobStatusFailed:     "failed",
	}
	for js, want := range cases {
		if got := js.String(); got != want {
			t.Errorf("%v.String() = %q; want %q", js, got, want)
		}
	}
}

func TestJobStatus_Terminal(t *testing.T) {
	if JobStatusCompleted.IsTerminal() != true || JobStatusFailed.IsTerminal() != true {
		t.Error("Completed and Failed must be terminal")
	}
	if JobStatusQueued.IsTerminal() || JobStatusInProgress.IsTerminal() {
		t.Error("Queued and InProgress must NOT be terminal")
	}
}
