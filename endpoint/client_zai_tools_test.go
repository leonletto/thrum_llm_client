package endpoint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestZaiClient_Chat_SendsTools verifies that tools[] reaches the wire correctly.
func TestZaiClient_Chat_SendsTools(t *testing.T) {
	var capturedReq ZaiRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := ZaiResponse{
			ID:      "test-id",
			Model:   capturedReq.Model,
			Choices: []ZaiChoice{{Index: 0, Message: ZaiRespMsg{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			Usage:   ZaiUsage{TotalTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tools := []ToolDefinition{
		{Name: "get_weather", Description: "Get current weather", Schema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`)},
	}

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	_, err := client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "What's the weather?"},
	}, &ChatOptions{Tools: tools})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if len(capturedReq.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(capturedReq.Tools))
	}
	if capturedReq.Tools[0].Type != "function" {
		t.Errorf("Tools[0].Type = %q, want %q", capturedReq.Tools[0].Type, "function")
	}
	if capturedReq.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Tools[0].Function.Name = %q, want %q", capturedReq.Tools[0].Function.Name, "get_weather")
	}
	if capturedReq.Tools[0].Function.Description != "Get current weather" {
		t.Errorf("Tools[0].Function.Description = %q, want 'Get current weather'", capturedReq.Tools[0].Function.Description)
	}
}

// TestZaiClient_Chat_ParsesToolCalls verifies tool_calls in response populate ChatResponse.ToolCalls.
func TestZaiClient_Chat_ParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ZaiResponse{
			ID:    "test-id",
			Model: "GLM-4.7",
			Choices: []ZaiChoice{{
				Index: 0,
				Message: ZaiRespMsg{
					Role:    "assistant",
					Content: "",
					ToolCalls: []zaiToolCall{
						{
							ID:   "call_abc123",
							Type: "function",
							Function: zaiToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"location":"London"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ZaiUsage{TotalTokens: 20},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	chatResp, err := client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "What's the weather in London?"},
	}, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if len(chatResp.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(chatResp.ToolCalls))
	}
	tc := chatResp.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc123")
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "get_weather")
	}
	// Verify args is valid JSON RawMessage
	var args map[string]string
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatalf("ToolCall.Args is not valid JSON: %v", err)
	}
	if args["location"] != "London" {
		t.Errorf("ToolCall.Args[location] = %q, want %q", args["location"], "London")
	}
}

// TestZaiClient_ChatStream_ParsesToolCallDeltas verifies tool-call deltas accumulate via return value.
func TestZaiClient_ChatStream_ParsesToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_xyz","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
			`data: {"id":"2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]}}]}`,
			`data: {"id":"3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"Paris\"}"}}]}}]}`,
			`data: {"id":"4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n"))
		}
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	var textContent strings.Builder
	toolCalls, err := client.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "Weather in Paris?"},
	}, nil, func(chunk string) error {
		textContent.WriteString(chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if textContent.String() != "" {
		t.Errorf("text content = %q, want empty (tool call only)", textContent.String())
	}
	if len(toolCalls) != 1 {
		t.Fatalf("len(toolCalls) = %d, want 1", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "call_xyz" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_xyz")
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "get_weather")
	}
	var args map[string]string
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatalf("ToolCall.Args not valid JSON: %v", err)
	}
	if args["location"] != "Paris" {
		t.Errorf("ToolCall.Args[location] = %q, want %q", args["location"], "Paris")
	}
}

// TestZaiClient_ChatStream_ToolCallInvalidJSON_Errors verifies malformed args return error.
func TestZaiClient_ChatStream_ToolCallInvalidJSON_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"bad_tool","arguments":"not-valid-json"}}]}}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n"))
		}
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	_, err := client.ChatStream(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "test"},
	}, nil, func(chunk string) error { return nil })

	if err == nil {
		t.Fatal("ChatStream() expected error for invalid JSON args, got nil")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %q, want to contain 'not valid JSON'", err.Error())
	}
}

// TestZaiClient_Chat_ToolResultRoundTrip verifies assistant tool-call → tool-result message round-trip.
func TestZaiClient_Chat_ToolResultRoundTrip(t *testing.T) {
	var capturedReq ZaiRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := ZaiResponse{
			ID:    "test-id",
			Model: "GLM-4.7",
			Choices: []ZaiChoice{{
				Index:        0,
				Message:      ZaiRespMsg{Role: "assistant", Content: "The weather in London is sunny."},
				FinishReason: "stop",
			}},
			Usage: ZaiUsage{TotalTokens: 30},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	messages := []ChatMessage{
		{Role: "user", Content: "What's the weather in London?"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_abc", Name: "get_weather", Args: json.RawMessage(`{"location":"London"}`)},
			},
		},
		{Role: "tool", Content: "Sunny, 22 C", ToolCallID: "call_abc", Name: "get_weather"},
	}

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	chatResp, err := client.Chat(context.Background(), "GLM-4.7", messages, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !strings.Contains(chatResp.Content, "sunny") {
		t.Errorf("Content = %q, want to contain 'sunny'", chatResp.Content)
	}

	if len(capturedReq.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(capturedReq.Messages))
	}
	assistantMsg := capturedReq.Messages[1]
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("assistant message ToolCalls len = %d, want 1", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", assistantMsg.ToolCalls[0].ID, "call_abc")
	}
	toolResultMsg := capturedReq.Messages[2]
	if toolResultMsg.ToolCallID != "call_abc" {
		t.Errorf("tool result ToolCallID = %q, want %q", toolResultMsg.ToolCallID, "call_abc")
	}
}

// TestZaiClient_Chat_NoToolsBackcompat verifies existing chat still works without tools (regression).
func TestZaiClient_Chat_NoToolsBackcompat(t *testing.T) {
	var capturedReq ZaiRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		resp := ZaiResponse{
			ID:    "test-id",
			Model: "GLM-4.7",
			Choices: []ZaiChoice{
				{Index: 0, Message: ZaiRespMsg{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"},
			},
			Usage: ZaiUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewZaiClientWithEndpoint("test-key", server.URL)
	resp, err := client.Chat(context.Background(), "GLM-4.7", []ChatMessage{
		{Role: "user", Content: "Say hello"},
	}, nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty, got %d", len(resp.ToolCalls))
	}
	if len(capturedReq.Tools) != 0 {
		t.Errorf("tools[] should not be sent when empty, got %d", len(capturedReq.Tools))
	}
}
