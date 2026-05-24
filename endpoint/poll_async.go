package endpoint

import (
	"context"
	"fmt"
	"time"
)

// pollAsync is the shared async-poll loop used by all video adapters.
// It calls fetchOnce immediately, then on a fixed interval until the
// returned VideoJob's Status reports terminal (Completed or Failed),
// the context cancels, MaxWait elapses, or fetchOnce returns an error.
//
// Error semantics:
//   - On MaxWait elapsed: returns an error wrapping ErrPollTimeout
//     with a "max wait N exceeded" context string (NOT ErrTimeout —
//     that sentinel is reserved for HTTP 408/504 from a single
//     request).
//   - On ctx cancel: returns ctx.Err() (context.Canceled or
//     context.DeadlineExceeded).
//   - On fetchOnce returning a non-nil error: propagates it unwrapped.
//   - On fetchOnce returning (nil, nil): returns an explicit error
//     so the caller never sees a silent stall on a misbehaving
//     adapter.
//
// This is the no-progress wrapper around pollAsyncWithProgress.
func pollAsync(
	ctx context.Context,
	fetchOnce func(context.Context) (*VideoJob, error),
	opts PollOptions,
) (*VideoJob, error) {
	return pollAsyncWithProgress(ctx, fetchOnce, opts, nil)
}

// pollAsyncWithProgress is the progress-aware variant. When
// onProgress is non-nil, every successful fetch emits one
// ProgressPolling event with the latest Status and Progress
// (provider integer in [0,100] or -1 when not reported).
//
// The loop ordering exactly mirrors the non-progress pollAsync:
// fetch first, then the optional progress emit, then the terminal
// check, then the ctx/sleep select. A pre-cancelled context still
// gets one fetch attempt.
func pollAsyncWithProgress(
	ctx context.Context,
	fetchOnce func(context.Context) (*VideoJob, error),
	opts PollOptions,
	onProgress ProgressFn,
) (*VideoJob, error) {
	opts.applyDefaults()

	deadline := time.Now().Add(opts.MaxWait)

	var last *VideoJob
	for {
		job, err := fetchOnce(ctx)
		if err != nil {
			return nil, err
		}
		if job == nil {
			return nil, fmt.Errorf("pollAsync: fetchOnce returned nil job without error (adapter contract violation)")
		}
		last = job

		if onProgress != nil {
			onProgress(ProgressEvent{
				Phase:   ProgressPolling,
				Status:  job.Status,
				Percent: job.Progress,
			})
		}

		if job.Status.IsTerminal() {
			return job, nil
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return last, fmt.Errorf("%w: max wait %v exceeded", ErrPollTimeout, opts.MaxWait)
		}
		wait := opts.Interval
		if wait > remaining {
			wait = remaining
		}

		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(wait):
		}
	}
}
