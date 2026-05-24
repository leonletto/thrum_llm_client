package endpoint

// JobStatus is a unified status enum for asynchronous provider jobs
// (currently used only as a stub for the upcoming video-generation
// epic; image generation is synchronous and does not consume this).
//
// Each video-generation adapter maps its provider-native status
// strings to one of these values:
//   - Z.ai "PROCESSING"   → JobStatusInProgress
//   - Z.ai "SUCCESS"      → JobStatusCompleted
//   - Z.ai "FAIL"         → JobStatusFailed
//   - OpenAI "queued"     → JobStatusQueued
//   - OpenAI "in_progress"→ JobStatusInProgress
//   - OpenAI "completed"  → JobStatusCompleted
//   - OpenAI "failed"     → JobStatusFailed
//   - OpenRouter (TBD; likely mirrors OpenAI)
type JobStatus int

const (
	// JobStatusUnknown is the zero value, signaling the status
	// could not be determined (parse failure, missing field, etc.).
	JobStatusUnknown JobStatus = iota
	JobStatusQueued
	JobStatusInProgress
	JobStatusCompleted
	JobStatusFailed
)

// String returns the lowercase canonical name of the status.
func (s JobStatus) String() string {
	switch s {
	case JobStatusQueued:
		return "queued"
	case JobStatusInProgress:
		return "in_progress"
	case JobStatusCompleted:
		return "completed"
	case JobStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// IsTerminal reports whether the status is final (job will not
// transition further). Completed and Failed are terminal; the
// others are not.
func (s JobStatus) IsTerminal() bool {
	return s == JobStatusCompleted || s == JobStatusFailed
}
