package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// --- predicate unit tests ---------------------------------------------------

func TestZaiEmptyToolCallPredicate_NotZai_DoesNotFire(t *testing.T) {
	p := zaiEmptyToolCallPredicate{}
	req := ChatRequest{Provider: "openai"}
	retry, _ := p.ShouldRetry(req, nil, fmt.Errorf("%w: anything", ErrZaiEmptyToolCall))
	if retry {
		t.Fatalf("predicate should not fire for non-zai provider")
	}
}

func TestZaiEmptyToolCallPredicate_FiresOnSentinelError(t *testing.T) {
	p := zaiEmptyToolCallPredicate{}
	req := ChatRequest{Provider: "zai"}
	wrapped := fmt.Errorf("%w: tool-call %q args not valid JSON: unexpected end of JSON input", ErrZaiEmptyToolCall, "browse_codebase")
	retry, reason := p.ShouldRetry(req, nil, wrapped)
	if !retry {
		t.Fatalf("predicate should fire on ErrZaiEmptyToolCall, got reason=%q", reason)
	}
	if reason == "" {
		t.Errorf("expected non-empty reason")
	}
}

func TestZaiEmptyToolCallPredicate_FiresOnFinishReasonWithNoUsableArgs(t *testing.T) {
	p := zaiEmptyToolCallPredicate{}
	req := ChatRequest{Provider: "zai"}
	resp := &ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls:    nil, // claims tool-call, but none present
	}
	if retry, _ := p.ShouldRetry(req, resp, nil); !retry {
		t.Fatalf("expected fire when finish_reason=tool_calls with empty ToolCalls")
	}

	respWithEmptyArgs := &ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []ToolCall{
			{ID: "1", Name: "x", Args: json.RawMessage(``)},
			{ID: "2", Name: "y", Args: json.RawMessage(`   `)},
		},
	}
	if retry, _ := p.ShouldRetry(req, respWithEmptyArgs, nil); !retry {
		t.Fatalf("expected fire when all tool-calls have empty args")
	}

	respWithBadArgs := &ChatResponse{
		FinishReason: "function_call",
		ToolCalls:    []ToolCall{{ID: "1", Name: "x", Args: json.RawMessage(`{not_json`)}},
	}
	if retry, _ := p.ShouldRetry(req, respWithBadArgs, nil); !retry {
		t.Fatalf("expected fire for finish_reason=function_call with malformed args")
	}
}

func TestZaiEmptyToolCallPredicate_DoesNotFireOnHappyPath(t *testing.T) {
	p := zaiEmptyToolCallPredicate{}
	req := ChatRequest{Provider: "zai"}

	// Plain text response, no tool-call.
	if retry, _ := p.ShouldRetry(req, &ChatResponse{FinishReason: "stop", Content: "hi"}, nil); retry {
		t.Errorf("predicate fired on plain text response")
	}

	// Tool-call response with valid args.
	resp := &ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls:    []ToolCall{{ID: "1", Name: "x", Args: json.RawMessage(`{"location":"London"}`)}},
	}
	if retry, _ := p.ShouldRetry(req, resp, nil); retry {
		t.Errorf("predicate fired on valid tool-call args")
	}

	// At least one valid args among many is enough.
	respMixed := &ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls: []ToolCall{
			{ID: "1", Name: "x", Args: json.RawMessage(``)},
			{ID: "2", Name: "y", Args: json.RawMessage(`{"a":1}`)},
		},
	}
	if retry, _ := p.ShouldRetry(req, respMixed, nil); retry {
		t.Errorf("predicate fired despite at least one valid tool-call")
	}
}

// --- DefaultRetryPolicy ----------------------------------------------------

func TestDefaultRetryPolicy_Shape(t *testing.T) {
	p := DefaultRetryPolicy()
	if p == nil {
		t.Fatalf("DefaultRetryPolicy returned nil")
	}
	if p.MaxRetries != 1 {
		t.Errorf("MaxRetries = %d, want 1", p.MaxRetries)
	}
	if len(p.Predicates) != 1 {
		t.Fatalf("expected 1 predicate, got %d", len(p.Predicates))
	}
	if p.Predicates[0].Name() != "zai_empty_tool_call" {
		t.Errorf("default predicate name = %q, want %q", p.Predicates[0].Name(), "zai_empty_tool_call")
	}
}

// --- Integration: retry against httptest.Server ----------------------------

// zaiServerEmptyThenGood returns an empty-args tool-call on attempt 1 and a
// well-formed tool-call response on attempt 2+.
func zaiServerEmptyThenGood(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		args := `{"location":"London"}`
		if n == 1 {
			args = "" // the known bad shape
		}
		resp := ZaiResponse{
			ID:    "x",
			Model: "GLM-4.7",
			Choices: []ZaiChoice{{
				Index: 0,
				Message: ZaiRespMsg{
					Role: "assistant",
					ToolCalls: []zaiToolCall{{
						ID:       "call_1",
						Type:     "function",
						Function: zaiToolCallFunction{Name: "get_weather", Arguments: args},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ZaiUsage{TotalTokens: 10},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &attempts
}

func TestRetryPolicy_DefaultRetriesZaiEmptyToolCall(t *testing.T) {
	srv, attempts := zaiServerEmptyThenGood(t)
	defer srv.Close()

	var events []RetryEvent
	policy := DefaultRetryPolicy()
	policy.OnRetry = func(e RetryEvent) { events = append(events, e) }

	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/", // override since httptest URL is bare
		RetryPolicy: policy,
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}

	resp, err := c.Chat(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, &ChatOptions{
		Tools: []ToolDefinition{{Name: "get_weather", Schema: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("server saw %d attempts, want 2", got)
	}
	if len(resp.ToolCalls) != 1 || string(resp.ToolCalls[0].Args) != `{"location":"London"}` {
		t.Errorf("final response did not carry good tool-call: %+v", resp.ToolCalls)
	}
	if len(events) != 1 {
		t.Fatalf("OnRetry fired %d times, want 1", len(events))
	}
	if events[0].PredicateName != "zai_empty_tool_call" {
		t.Errorf("event PredicateName = %q", events[0].PredicateName)
	}
	if events[0].Attempt != 1 {
		t.Errorf("event Attempt = %d, want 1", events[0].Attempt)
	}
	if events[0].Reason == "" {
		t.Error("event Reason is empty")
	}
	if events[0].LatencyDelta <= 0 {
		t.Error("event LatencyDelta should be > 0")
	}
}

func TestRetryPolicy_NilPolicyAppliesDefault(t *testing.T) {
	srv, attempts := zaiServerEmptyThenGood(t)
	defer srv.Close()

	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		// RetryPolicy intentionally nil → DefaultRetryPolicy applied.
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := c.Chat(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2 (default policy should retry once)", got)
	}
}

func TestRetryPolicy_ExplicitOptOut(t *testing.T) {
	srv, attempts := zaiServerEmptyThenGood(t)
	defer srv.Close()

	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		RetryPolicy: &RetryPolicy{}, // MaxRetries=0, no predicates
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	_, err = c.Chat(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error from first failed attempt without retry")
	}
	if !errors.Is(err, ErrZaiEmptyToolCall) {
		t.Errorf("err = %v; want chain to ErrZaiEmptyToolCall", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (retry disabled)", got)
	}
}

func TestRetryPolicy_MaxRetriesZeroExplicit(t *testing.T) {
	srv, attempts := zaiServerEmptyThenGood(t)
	defer srv.Close()

	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		RetryPolicy: &RetryPolicy{
			Predicates: []RetryPredicate{zaiEmptyToolCallPredicate{}},
			MaxRetries: 0,
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	if _, err := c.Chat(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, nil); err == nil {
		t.Fatal("expected error: MaxRetries=0 means no retries")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

// --- Custom user-defined predicate ----------------------------------------

type alwaysRetryPredicate struct {
	fired atomic.Int32
}

func (p *alwaysRetryPredicate) Name() string { return "always_retry" }
func (p *alwaysRetryPredicate) ShouldRetry(_ ChatRequest, _ *ChatResponse, err error) (bool, string) {
	if err != nil && errors.Is(err, ErrServiceUnavailable) {
		p.fired.Add(1)
		return true, "503"
	}
	return false, ""
}

func TestRetryPolicy_CustomPredicate(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			http.Error(w, "fake-503 transient", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ZaiResponse{
			ID:    "x",
			Model: "GLM-4.7",
			Choices: []ZaiChoice{{
				Index: 0, Message: ZaiRespMsg{Role: "assistant", Content: "ok"}, FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	pred := &alwaysRetryPredicate{}
	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		RetryPolicy: &RetryPolicy{Predicates: []RetryPredicate{pred}, MaxRetries: 2},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	resp, err := c.Chat(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q", resp.Content)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if pred.fired.Load() == 0 {
		t.Errorf("custom predicate never fired")
	}
}

// --- Stream retry ----------------------------------------------------------

// zaiStreamServer returns:
//   - attempt 1: empty SSE (no chunks, no tool-calls) → predicate fires.
//   - attempt 2: a clean text stream.
func zaiStreamServerEmptyThenText(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		if n == 1 {
			// Stream a tool-call delta with empty arguments and finish_reason=tool_calls.
			// The adapter accumulates "" → unmarshal fails → wrapped in ErrZaiEmptyToolCall.
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"x","arguments":""}}]},"finish_reason":"tool_calls"}]}`)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"content":"hello"}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	return srv, &attempts
}

func TestRetryPolicy_StreamRetriesBeforeFirstChunk(t *testing.T) {
	srv, attempts := zaiStreamServerEmptyThenText(t)
	defer srv.Close()

	var events []RetryEvent
	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		RetryPolicy: &RetryPolicy{
			Predicates: []RetryPredicate{zaiEmptyToolCallPredicate{}},
			MaxRetries: 1,
			OnRetry:    func(e RetryEvent) { events = append(events, e) },
		},
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}

	var collected strings.Builder
	tcs, err := c.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}},
		&ChatOptions{Tools: []ToolDefinition{{Name: "x", Schema: json.RawMessage(`{}`)}}},
		func(chunk string) error { collected.WriteString(chunk); return nil })
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
	if collected.String() != "hello world" {
		t.Errorf("collected = %q, want %q", collected.String(), "hello world")
	}
	if len(tcs) != 0 {
		t.Errorf("tool-calls on retry attempt should be empty, got %v", tcs)
	}
	if len(events) != 1 {
		t.Errorf("OnRetry fired %d times, want 1", len(events))
	}
}

// zaiStreamServerChunkThenError emits one text chunk, then closes the
// connection mid-stream. The adapter does NOT raise the empty tool-call
// sentinel, but no retry is permitted because a chunk already fired.
func TestRetryPolicy_StreamNoRetryAfterFirstChunk(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Emit one chunk then a tool-call delta with bad args → error after chunk delivered.
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`)
		flusher.Flush()
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"x","arguments":""}}]},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c, err := NewChatClient(ChatClientConfig{
		EndpointURL: srv.URL,
		Provider:    "zai",
		APIKey:      "test",
		ChatPath:    "/",
		RetryPolicy: DefaultRetryPolicy(),
	})
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}
	_, err = c.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{{Role: "user", Content: "hi"}}, nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error to surface after first chunk delivered")
	}
	if !errors.Is(err, ErrZaiEmptyToolCall) {
		t.Errorf("err = %v; want chain to ErrZaiEmptyToolCall (no retry should have happened)", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry after first chunk)", got)
	}
}
