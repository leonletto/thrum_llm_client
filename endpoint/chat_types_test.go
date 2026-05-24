package endpoint

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChatMessageJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		msg      ChatMessage
		wantJSON string
	}{
		{
			name:     "system message",
			msg:      ChatMessage{Role: "system", Content: "You are a helpful assistant."},
			wantJSON: `{"role":"system","content":"You are a helpful assistant."}`,
		},
		{
			name:     "user message",
			msg:      ChatMessage{Role: "user", Content: "Hello!"},
			wantJSON: `{"role":"user","content":"Hello!"}`,
		},
		{
			name:     "assistant message",
			msg:      ChatMessage{Role: "assistant", Content: "Hi there!"},
			wantJSON: `{"role":"assistant","content":"Hi there!"}`,
		},
		{
			name:     "empty content",
			msg:      ChatMessage{Role: "user", Content: ""},
			wantJSON: `{"role":"user","content":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if string(data) != tt.wantJSON {
				t.Errorf("got %s, want %s", string(data), tt.wantJSON)
			}

			// Test round-trip
			var decoded ChatMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if !reflect.DeepEqual(decoded, tt.msg) {
				t.Errorf("round-trip failed: got %+v, want %+v", decoded, tt.msg)
			}
		})
	}
}

func TestChatOptionsJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		opts ChatOptions
	}{
		{
			name: "full options",
			opts: ChatOptions{
				MaxTokens:      512,
				Temperature:    0.7,
				Stream:         true,
				Stop:           []string{"\n\n", "END"},
				EnableThinking: true,
				ThinkingBudget: 1000,
			},
		},
		{
			name: "minimal options",
			opts: ChatOptions{MaxTokens: 100},
		},
		{
			name: "empty options",
			opts: ChatOptions{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.opts)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded ChatOptions
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			// Compare fields
			if decoded.MaxTokens != tt.opts.MaxTokens {
				t.Errorf("MaxTokens: got %d, want %d", decoded.MaxTokens, tt.opts.MaxTokens)
			}
			if decoded.Temperature != tt.opts.Temperature {
				t.Errorf("Temperature: got %f, want %f", decoded.Temperature, tt.opts.Temperature)
			}
			if decoded.Stream != tt.opts.Stream {
				t.Errorf("Stream: got %v, want %v", decoded.Stream, tt.opts.Stream)
			}
			if decoded.EnableThinking != tt.opts.EnableThinking {
				t.Errorf("EnableThinking: got %v, want %v", decoded.EnableThinking, tt.opts.EnableThinking)
			}
			if decoded.ThinkingBudget != tt.opts.ThinkingBudget {
				t.Errorf("ThinkingBudget: got %d, want %d", decoded.ThinkingBudget, tt.opts.ThinkingBudget)
			}
		})
	}
}

func TestTokenUsageJSONSerialization(t *testing.T) {
	usage := TokenUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded TokenUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != usage {
		t.Errorf("round-trip failed: got %+v, want %+v", decoded, usage)
	}
}

func TestChatResponseJSONSerialization(t *testing.T) {
	resp := ChatResponse{
		Content:      "Hello, how can I help you?",
		Model:        "gpt-4",
		TokensUsed:   TokenUsage{PromptTokens: 10, CompletionTokens: 8, TotalTokens: 18},
		FinishReason: "stop",
		Thinking:     "Let me think about this...",
		Metrics: &ResponseMetrics{
			GenerationThroughput: 50.5,
			RequestsRunning:      3,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Content != resp.Content {
		t.Errorf("Content: got %q, want %q", decoded.Content, resp.Content)
	}
	if decoded.Model != resp.Model {
		t.Errorf("Model: got %q, want %q", decoded.Model, resp.Model)
	}
	if decoded.FinishReason != resp.FinishReason {
		t.Errorf("FinishReason: got %q, want %q", decoded.FinishReason, resp.FinishReason)
	}
	if decoded.Thinking != resp.Thinking {
		t.Errorf("Thinking: got %q, want %q", decoded.Thinking, resp.Thinking)
	}
}

func TestResponseMetricsJSONSerialization(t *testing.T) {
	metrics := ResponseMetrics{
		GenerationThroughput:      78.5,
		PromptThroughput:          120.3,
		KVCacheUsagePercent:       0.45,
		PrefixCacheHitRatePercent: 0.82,
		RequestsRunning:           5,
		RequestsWaiting:           2,
		ProviderSpecific: map[string]any{
			"custom_field": "value",
			"numeric":      42,
		},
	}

	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ResponseMetrics
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.GenerationThroughput != metrics.GenerationThroughput {
		t.Errorf("GenerationThroughput: got %f, want %f", decoded.GenerationThroughput, metrics.GenerationThroughput)
	}
	if decoded.PromptThroughput != metrics.PromptThroughput {
		t.Errorf("PromptThroughput: got %f, want %f", decoded.PromptThroughput, metrics.PromptThroughput)
	}
	if decoded.RequestsRunning != metrics.RequestsRunning {
		t.Errorf("RequestsRunning: got %d, want %d", decoded.RequestsRunning, metrics.RequestsRunning)
	}
}

func TestChatOptionsOmitEmpty(t *testing.T) {
	// Empty options should have minimal JSON
	opts := ChatOptions{}
	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Should be empty object since all fields have omitempty
	if string(data) != "{}" {
		t.Errorf("empty options should serialize to {}, got %s", string(data))
	}
}

func TestChatResponseWithNilMetrics(t *testing.T) {
	resp := ChatResponse{
		Content:      "Test response",
		Model:        "test-model",
		TokensUsed:   TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		FinishReason: "stop",
		Metrics:      nil, // nil metrics
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Metrics should be omitted when nil
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if _, ok := decoded["metrics"]; ok {
		t.Error("nil metrics should be omitted from JSON")
	}
}
