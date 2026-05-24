// Package endpoint provides LLM endpoint management with multi-provider support.
package endpoint

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ChatRequest is the inspectable, immutable view of a chat call passed to
// retry predicates. Predicates MUST treat it as read-only.
type ChatRequest struct {
	// Provider is the active adapter's ProviderName() (e.g. "zai", "openai").
	Provider string
	// Model is the model name AFTER registry resolution.
	Model string
	// Messages is the message list sent to the provider.
	Messages []ChatMessage
	// Options is the per-call options pointer (may be nil).
	Options *ChatOptions
	// Stream is true when the call originated from ChatStream.
	Stream bool
}

// RetryPredicate inspects a completed response (or error) and decides whether
// to retry the request. Predicates MUST be pure — no state, no I/O. The
// interface is intentionally narrow so new checks can be added by writing a
// new predicate type without modifying any existing struct.
type RetryPredicate interface {
	// Name returns a stable identifier used in logs, metrics, and
	// RetryEvent.PredicateName. It MUST be unique within a policy and
	// stable across versions.
	Name() string

	// ShouldRetry returns (true, reason) if the request should be retried.
	// reason is a short human-readable string surfaced via RetryEvent.
	// resp may be nil when err != nil; predicates MUST handle both.
	ShouldRetry(req ChatRequest, resp *ChatResponse, err error) (retry bool, reason string)
}

// RetryEvent is emitted via RetryPolicy.OnRetry whenever a retry decision
// fires. Consumers may use it for logging, metrics, or tracing. Adding new
// fields here is a backwards-compatible change.
type RetryEvent struct {
	// Attempt is 1-based; 1 = first retry (i.e. second total attempt).
	Attempt int
	// PredicateName is the firing predicate's Name().
	PredicateName string
	// Reason is the predicate's human-readable reason.
	Reason string
	// LatencyDelta is the wall-clock duration of the failed attempt.
	LatencyDelta time.Duration
}

// RetryPolicy controls transport-layer retry behavior on UnifiedChatClient.
//
// When ChatClientConfig.RetryPolicy is nil, DefaultRetryPolicy() is applied.
// To explicitly disable retries, set RetryPolicy to a non-nil value with
// MaxRetries == 0 (or with a nil/empty Predicates slice).
//
// Retry semantics:
//   - Predicate registry is OR — any firing predicate triggers a retry.
//   - Total attempts = 1 + MaxRetries.
//   - Predicates fire on both successful responses and errors.
//   - For ChatStream, retries are permitted ONLY before the first text
//     chunk has been delivered to the consumer's callback. Once any chunk
//     fires, the stream is committed and the second-attempt result (if
//     any) is returned naturally without further retry.
type RetryPolicy struct {
	// Predicates is evaluated in order; first firing predicate wins.
	Predicates []RetryPredicate
	// MaxRetries is the maximum number of retry attempts after the first.
	// 0 means no retries.
	MaxRetries int
	// OnRetry, if non-nil, is invoked once per retry decision.
	// It MUST NOT block the request meaningfully.
	OnRetry func(RetryEvent)
}

// shouldRetry walks the predicate list and returns the first firing one.
// Returns (false, "", "") if no predicate fires.
func (p *RetryPolicy) shouldRetry(req ChatRequest, resp *ChatResponse, err error) (bool, string, string) {
	if p == nil {
		return false, "", ""
	}
	for _, pred := range p.Predicates {
		if pred == nil {
			continue
		}
		if retry, reason := pred.ShouldRetry(req, resp, err); retry {
			return true, pred.Name(), reason
		}
	}
	return false, "", ""
}

// DefaultRetryPolicy returns the shipping default policy: 1 retry on the
// zai empty / malformed tool-call failure mode. This is the policy applied
// when ChatClientConfig.RetryPolicy is left nil.
//
// Consumers may extend it (append additional predicates), replace it, or
// disable retries by setting an explicit empty policy on the config.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		Predicates: []RetryPredicate{zaiEmptyToolCallPredicate{}},
		MaxRetries: 1,
	}
}

// zaiEmptyToolCallPredicate fires when zai returns a tool-call response in
// which no advertised tool-call carries non-empty, valid-JSON arguments.
//
// This is a known sporadic failure mode of the zai endpoint observed during
// sustained live tool-using runs: finish_reason is "tool_calls" (or
// "function_call") yet either:
//   - the adapter rejected the response because tool-call arguments
//     failed to parse as JSON (typically empty string -> "unexpected end of
//     JSON input"), wrapped with sentinel ErrZaiEmptyToolCall, or
//   - the response has finish_reason indicating a tool-call but no
//     tool_calls in the message at all.
//
// A single retry recovers the call in practice.
type zaiEmptyToolCallPredicate struct{}

func (zaiEmptyToolCallPredicate) Name() string { return "zai_empty_tool_call" }

func (zaiEmptyToolCallPredicate) ShouldRetry(req ChatRequest, resp *ChatResponse, err error) (bool, string) {
	if req.Provider != "zai" {
		return false, ""
	}

	// Path 1: adapter returned a structured rejection.
	if err != nil && errors.Is(err, ErrZaiEmptyToolCall) {
		return true, "zai returned empty/malformed tool-call arguments"
	}

	// Path 2: response delivered, but finish_reason claims a tool-call and
	// no tool-call in the response carries usable arguments.
	if err == nil && resp != nil {
		fr := resp.FinishReason
		if fr == "tool_calls" || fr == "function_call" {
			if !anyValidToolCallArgs(resp.ToolCalls) {
				return true, "zai finish_reason=" + fr + " with no usable tool-call arguments"
			}
		}
	}

	return false, ""
}

// anyValidToolCallArgs reports whether at least one tool call carries
// non-empty, valid-JSON arguments.
func anyValidToolCallArgs(tcs []ToolCall) bool {
	if len(tcs) == 0 {
		return false
	}
	for _, tc := range tcs {
		raw := strings.TrimSpace(string(tc.Args))
		if raw == "" {
			continue
		}
		var probe any
		if err := json.Unmarshal([]byte(raw), &probe); err == nil {
			return true
		}
	}
	return false
}
