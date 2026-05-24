package endpoint

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollAsync_TerminalsImmediately(t *testing.T) {
	calls := atomic.Int32{}
	got, err := pollAsync(context.Background(),
		func(_ context.Context) (*VideoJob, error) {
			calls.Add(1)
			return &VideoJob{Status: JobStatusCompleted, ID: "x"}, nil
		},
		PollOptions{Interval: 10 * time.Millisecond, MaxWait: 1 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != JobStatusCompleted {
		t.Errorf("got %v", got.Status)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d; want 1", calls.Load())
	}
}

func TestPollAsync_LoopsUntilTerminal(t *testing.T) {
	calls := atomic.Int32{}
	_, err := pollAsync(context.Background(),
		func(_ context.Context) (*VideoJob, error) {
			n := calls.Add(1)
			if n < 3 {
				return &VideoJob{Status: JobStatusInProgress, Progress: int(n) * 33}, nil
			}
			return &VideoJob{Status: JobStatusCompleted}, nil
		},
		PollOptions{Interval: 5 * time.Millisecond, MaxWait: 1 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d; want 3", calls.Load())
	}
}

func TestPollAsync_TimeoutReturnsErrPollTimeout(t *testing.T) {
	_, err := pollAsync(context.Background(),
		func(_ context.Context) (*VideoJob, error) {
			return &VideoJob{Status: JobStatusInProgress}, nil
		},
		PollOptions{Interval: 5 * time.Millisecond, MaxWait: 30 * time.Millisecond},
	)
	if !errors.Is(err, ErrPollTimeout) {
		t.Errorf("got %v; want ErrPollTimeout", err)
	}
	if errors.Is(err, ErrTimeout) {
		t.Error("ErrPollTimeout must not unwrap to ErrTimeout")
	}
}

func TestPollAsync_NilJobWithoutErrorIsExplicitFailure(t *testing.T) {
	_, err := pollAsync(context.Background(),
		func(_ context.Context) (*VideoJob, error) { return nil, nil },
		PollOptions{Interval: 1 * time.Millisecond, MaxWait: 1 * time.Second},
	)
	if err == nil {
		t.Fatal("expected error for nil job + nil error")
	}
	if errors.Is(err, ErrPollTimeout) {
		t.Errorf("expected explicit nil-job error, not poll timeout; got %v", err)
	}
}

func TestPollAsync_PropagatesContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := pollAsync(ctx,
		func(_ context.Context) (*VideoJob, error) {
			return &VideoJob{Status: JobStatusInProgress}, nil
		},
		PollOptions{Interval: 5 * time.Millisecond, MaxWait: 1 * time.Second},
	)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v; want context.Canceled", err)
	}
}

func TestPollAsync_PropagatesFetchError(t *testing.T) {
	want := fmt.Errorf("upstream broken")
	_, err := pollAsync(context.Background(),
		func(_ context.Context) (*VideoJob, error) { return nil, want },
		PollOptions{Interval: 5 * time.Millisecond, MaxWait: 1 * time.Second},
	)
	if !errors.Is(err, want) {
		t.Errorf("got %v; want %v", err, want)
	}
}

func TestPollAsyncWithProgress_EmitsPollingEvents(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context) (*VideoJob, error) {
		calls++
		if calls < 3 {
			return &VideoJob{Status: JobStatusInProgress, Progress: calls * 30}, nil
		}
		return &VideoJob{Status: JobStatusCompleted, Progress: 100}, nil
	}

	var events []ProgressEvent
	cb := func(e ProgressEvent) { events = append(events, e) }

	job, err := pollAsyncWithProgress(context.Background(), fetch, PollOptions{
		Interval: 5 * time.Millisecond,
		MaxWait:  500 * time.Millisecond,
	}, cb)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if job.Status != JobStatusCompleted {
		t.Errorf("got status %v; want completed", job.Status)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events; want 3", len(events))
	}
	for i, e := range events {
		if e.Phase != ProgressPolling {
			t.Errorf("event[%d].Phase = %v; want ProgressPolling", i, e.Phase)
		}
	}
	if events[0].Status != JobStatusInProgress || events[0].Percent != 30 {
		t.Errorf("event[0] = %+v; want {Polling, InProgress, 30}", events[0])
	}
	if events[2].Status != JobStatusCompleted || events[2].Percent != 100 {
		t.Errorf("event[2] = %+v; want {Polling, Completed, 100}", events[2])
	}
}

func TestPollAsyncWithProgress_NilCallbackBehavesAsPollAsync(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context) (*VideoJob, error) {
		calls++
		if calls < 2 {
			return &VideoJob{Status: JobStatusInProgress}, nil
		}
		return &VideoJob{Status: JobStatusCompleted}, nil
	}
	job, err := pollAsyncWithProgress(context.Background(), fetch, PollOptions{
		Interval: 1 * time.Millisecond,
		MaxWait:  100 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if job.Status != JobStatusCompleted {
		t.Errorf("got %v; want completed", job.Status)
	}
}
