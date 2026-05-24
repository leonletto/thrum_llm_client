package endpoint

// ProgressPhase identifies which phase of a download-bearing
// generation call a ProgressEvent belongs to.
type ProgressPhase int

const (
	// ProgressPolling fires for each poll iteration of an async
	// job (video only — image is synchronous). The event's
	// Status field is the latest provider-reported JobStatus and
	// Percent is the provider's progress integer (0-100) when
	// available, -1 otherwise.
	ProgressPolling ProgressPhase = iota

	// ProgressDownloading fires periodically while bytes are
	// being copied to disk. BytesDone is the cumulative byte
	// count; BytesTotal is the Content-Length header when the
	// server reported it, -1 otherwise. Percent is the integer
	// ratio when BytesTotal > 0, -1 otherwise.
	ProgressDownloading

	// ProgressComplete fires once at the end of a successful
	// generation, after disk writes complete. BytesDone reflects
	// the final byte count of the most recently written file.
	ProgressComplete
)

// String returns a stable, human-readable phase identifier.
func (p ProgressPhase) String() string {
	switch p {
	case ProgressPolling:
		return "polling"
	case ProgressDownloading:
		return "downloading"
	case ProgressComplete:
		return "complete"
	default:
		return "unknown"
	}
}

// ProgressEvent is a single progress notification delivered to
// the caller's ProgressFn during a generation call. Field
// population depends on Phase — consult phase docs above.
type ProgressEvent struct {
	Phase      ProgressPhase
	Status     JobStatus // populated during ProgressPolling
	Percent    int       // 0-100, or -1 if unknown
	BytesDone  int64     // populated during ProgressDownloading / ProgressComplete
	BytesTotal int64     // populated during ProgressDownloading when Content-Length is known; -1 otherwise
}

// ProgressFn is a caller-supplied callback receiving progress
// updates. The callback is invoked from the goroutine driving
// the work — callers are responsible for thread-safety if their
// implementation mutates shared state. The callback must NOT
// block on a long operation; use a buffered channel for fan-out
// if needed. Cancellation flows through ctx, not the callback's
// return value (it has none).
type ProgressFn func(ProgressEvent)
