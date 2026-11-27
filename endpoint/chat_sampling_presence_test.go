package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// captureSamplingRequest exercises the HTTP payload in each chat mode.
func captureSamplingRequest(t *testing.T, provider string, stream bool, opts *ChatOptions, extra map[string]any) map[string]json.RawMessage {
	t.Helper()
	captured := make(chan map[string]json.RawMessage, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		captured <- body
		if stream {
			if provider == "ollama" {
				_, _ = fmt.Fprintln(w, `{"done":true}`)
			} else {
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}
			return
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	client, err := NewChatClient(ChatClientConfig{EndpointURL: server.URL, Provider: provider, ExtraBody: extra, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	messages := []ChatMessage{{Role: "user", Content: "hi"}}
	if stream {
		_, err = client.ChatStream(ctx, "test", messages, opts, func(string) error { return nil })
	} else {
		_, err = client.Chat(ctx, "test", messages, opts)
	}
	if err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-captured:
		return body
	default:
		t.Fatal("expected one captured HTTP request")
		return nil
	}
}

func samplingFields(t *testing.T, provider string, body map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	if provider != "ollama" {
		return body
	}
	raw, present := body["options"]
	if !present {
		return map[string]json.RawMessage{}
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	return options
}

func assertSamplingJSON(t *testing.T, fields map[string]json.RawMessage, key string, want string) {
	t.Helper()
	raw, present := fields[key]
	if want == "" {
		if present {
			t.Errorf("%s unexpectedly present: %s", key, raw)
		}
		return
	}
	if !present {
		t.Fatalf("%s missing, want %s", key, want)
	}
	if string(raw) != want {
		t.Errorf("%s = %s, want %s", key, raw, want)
	}
}

func TestChatTemperatureWirePresence(t *testing.T) {
	cases := []struct {
		name string
		opts *ChatOptions
		want string
	}{
		{"nil", nil, ""},
		{"unset", &ChatOptions{}, ""},
		{"legacy_zero", &ChatOptions{Temperature: 0}, ""},
		{"other_options", &ChatOptions{MaxTokens: 25}, ""},
		{"explicit_zero", &ChatOptions{Temperature: 0, TemperatureSet: true}, "0"},
		{"nonzero", &ChatOptions{Temperature: 0.7}, "0.7"},
		{"nonzero_set", &ChatOptions{Temperature: 0.7, TemperatureSet: true}, "0.7"},
	}
	for _, provider := range []string{"anthropic", "openai", "ollama", "zai"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", provider, stream, tc.name), func(t *testing.T) {
					body := captureSamplingRequest(t, provider, stream, tc.opts, nil)
					assertSamplingJSON(t, samplingFields(t, provider, body), "temperature", tc.want)
					streamWant := fmt.Sprint(stream)
					if provider == "anthropic" && !stream {
						streamWant = ""
					}
					assertSamplingJSON(t, body, "stream", streamWant)
				})
			}
		}
	}
}

func TestChatSamplingExtraBodyPresence(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "ollama", "zai"} {
		for _, stream := range []bool{false, true} {
			for _, temperature := range []float64{0, 0.6} {
				t.Run(fmt.Sprintf("%s/stream=%t/override=%g", provider, stream, temperature), func(t *testing.T) {
					extra := map[string]any{"temperature": temperature, "top_p": 0, "seed": 0, "enabled": false, "text": "", "list": []any{}, "object": map[string]any{}, "nullable": nil}
					if provider == "ollama" {
						extra = map[string]any{"options": extra}
					}
					opts := &ChatOptions{Temperature: 0.9}
					if temperature != 0 {
						opts = &ChatOptions{TemperatureSet: true}
					}
					body := captureSamplingRequest(t, provider, stream, opts, extra)
					fields := samplingFields(t, provider, body)
					for key, want := range map[string]string{"temperature": fmt.Sprint(temperature), "top_p": "0", "seed": "0", "enabled": "false", "text": `""`, "list": "[]", "object": "{}", "nullable": "null"} {
						assertSamplingJSON(t, fields, key, want)
					}
				})
			}
		}
	}
}

func TestChatOptionsEmbeddingJSONCompatibility(t *testing.T) {
	type wrappedOptions struct {
		ChatOptions
		Name  string `json:"name"`
		Extra int    `json:"extra"`
	}
	got := wrappedOptions{ChatOptions: ChatOptions{Temperature: 0.5, TemperatureSet: true}, Name: "n", Extra: 3}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"temperature": "0.5", "name": `"n"`, "extra": "3"} {
		if string(fields[key]) != want {
			t.Errorf("marshal field %q = %s, want %s (body %s)", key, fields[key], want, raw)
		}
	}
	var decoded wrappedOptions
	if err := json.Unmarshal([]byte(`{"temperature":0.3,"name":"x","extra":5}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "x" || decoded.Extra != 5 || decoded.Temperature != 0.3 {
		t.Fatalf("unmarshal lost embedded or sibling fields: %+v", decoded)
	}

}

func TestAnthropicRequestEmbeddingJSONCompatibility(t *testing.T) {
	type wrappedRequest struct {
		AnthropicRequest
		Name  string `json:"name"`
		Extra int    `json:"extra"`
	}
	request := wrappedRequest{AnthropicRequest: AnthropicRequest{Model: "m", MaxTokens: 4, Temperature: 0.5}, Name: "n", Extra: 3}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"model": `"m"`, "name": `"n"`, "extra": "3"} {
		if string(fields[key]) != want {
			t.Errorf("AnthropicRequest marshal field %q = %s, want %s (body %s)", key, fields[key], want, raw)
		}
	}
	var decodedRequest wrappedRequest
	if err := json.Unmarshal([]byte(`{"model":"m2","max_tokens":5,"name":"x","extra":7}`), &decodedRequest); err != nil {
		t.Fatal(err)
	}
	if decodedRequest.Model != "m2" || decodedRequest.Name != "x" || decodedRequest.Extra != 7 {
		t.Fatalf("AnthropicRequest unmarshal lost embedded or sibling fields: %+v", decodedRequest)
	}
}

func TestChatOptionsJSONDecodedZeroRemainsUnset(t *testing.T) {
	var opts ChatOptions
	if err := json.Unmarshal([]byte(`{"temperature":0}`), &opts); err != nil {
		t.Fatal(err)
	}
	if opts.TemperatureSet {
		t.Fatal("JSON-decoded zero must not opt into explicit-zero behavior")
	}
	for _, provider := range []string{"anthropic", "openai", "ollama", "zai"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", provider, stream), func(t *testing.T) {
				body := captureSamplingRequest(t, provider, stream, &opts, nil)
				assertSamplingJSON(t, samplingFields(t, provider, body), "temperature", "")
			})
		}
	}
}

func TestChatTemperatureNegativeAndNaNMatchLegacyProviders(t *testing.T) {
	for _, provider := range []string{"anthropic", "ollama"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name  string
				value float64
			}{
				{name: "negative", value: -0.25},
				{name: "nan", value: math.NaN()},
			} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", provider, stream, tc.name), func(t *testing.T) {
					opts := &ChatOptions{Temperature: tc.value, TemperatureSet: true}
					body := captureSamplingRequest(t, provider, stream, opts, nil)
					assertSamplingJSON(t, samplingFields(t, provider, body), "temperature", "")
				})
			}
		}
	}

	for _, stream := range []bool{false, true} {
		body := captureSamplingRequest(t, "openai", stream, &ChatOptions{Temperature: -0.25, TemperatureSet: true}, nil)
		assertSamplingJSON(t, body, "temperature", "-0.25")
	}
	if _, err := json.Marshal(openaiChatRequest{Temperature: func() *float64 { value := math.NaN(); return &value }()}); err == nil {
		t.Fatal("OpenAI-compatible NaN temperature should retain its existing JSON marshal error")
	}
}

// Z.ai has always forwarded every nonzero value unchanged, including negative
// values, and failed to marshal non-finite ones.
func TestZaiChatTemperatureEdgeValues(t *testing.T) {
	negativeZero := math.Copysign(0, -1)
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			opts ChatOptions
			want string
		}{
			{name: "negative", opts: ChatOptions{Temperature: -0.25}, want: "-0.25"},
			{name: "negative_set", opts: ChatOptions{Temperature: -0.25, TemperatureSet: true}, want: "-0.25"},
			{name: "negative_zero", opts: ChatOptions{Temperature: negativeZero}, want: ""},
			{name: "negative_zero_set", opts: ChatOptions{Temperature: negativeZero, TemperatureSet: true}, want: "-0"},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				body := captureSamplingRequest(t, "zai", stream, &tc.opts, nil)
				assertSamplingJSON(t, body, "temperature", tc.want)
			})
		}
		for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			t.Run(fmt.Sprintf("stream=%t/%g", stream, value), func(t *testing.T) {
				client := NewZaiClientWithEndpoint("", "http://127.0.0.1:0")
				opts := &ChatOptions{Temperature: value}
				messages := []ChatMessage{{Role: "user", Content: "hi"}}
				var err error
				if stream {
					_, err = client.ChatStream(context.Background(), "test", messages, opts, func(string) error { return nil })
				} else {
					_, err = client.Chat(context.Background(), "test", messages, opts)
				}
				var unsupported *json.UnsupportedValueError
				if !errors.As(err, &unsupported) {
					t.Fatalf("error = %v, want JSON unsupported value error", err)
				}
			})
		}
	}
}

func TestZaiRequestBytes(t *testing.T) {
	const prefix = `{"model":"test","messages":[{"role":"user","content":"hi"}],"max_tokens":8000,`
	tools := []ToolDefinition{{Name: "lookup", Description: "Lookup", Schema: json.RawMessage(`{"type":"object"}`)}}
	for _, stream := range []bool{false, true} {
		suffix := fmt.Sprintf(`"stream":%t,"thinking":{"type":"disabled"}`, stream)
		for _, tc := range []struct {
			name string
			opts *ChatOptions
			want string
		}{
			{name: "unset", want: prefix + suffix + `}`},
			{
				name: "explicit_zero",
				opts: &ChatOptions{TemperatureSet: true},
				want: prefix + `"temperature":0,` + suffix + `}`,
			},
			{
				name: "nonzero_with_tools",
				opts: &ChatOptions{Temperature: 0.7, Tools: tools},
				want: prefix + `"temperature":0.7,` + suffix + `,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup","parameters":{"type":"object"}}}]}`,
			},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, tc.name), func(t *testing.T) {
				var body []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var err error
					body, err = io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					if stream {
						_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
						return
					}
					_, _ = fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
				}))
				defer server.Close()
				client := NewZaiClientWithEndpoint("", server.URL)
				messages := []ChatMessage{{Role: "user", Content: "hi"}}
				var err error
				if stream {
					_, err = client.ChatStream(context.Background(), "test", messages, tc.opts, func(string) error { return nil })
				} else {
					_, err = client.Chat(context.Background(), "test", messages, tc.opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if string(body) != tc.want {
					t.Fatalf("wire body = %s, want %s", body, tc.want)
				}
			})
		}
	}
}

func TestAnthropicRequestBytesMatchMain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   *ChatOptions
		stream bool
		want   string
	}{
		{
			name: "unset",
			want: `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"system":"rules"}`,
		},
		{
			name: "nonzero",
			opts: &ChatOptions{Temperature: 0.7},
			want: `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"system":"rules","temperature":0.7}`,
		},
		{
			name: "nonzero_with_stop_sequences",
			opts: &ChatOptions{Temperature: 0.7, Stop: []string{"STOP"}},
			want: `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"system":"rules","temperature":0.7,"stop_sequences":["STOP"]}`,
		},
		{
			name: "nonzero_with_thinking",
			opts: &ChatOptions{Temperature: 0.7, EnableThinking: true, ThinkingBudget: 5000},
			want: `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"system":"rules","temperature":0.7,"thinking":{"type":"enabled","budget_tokens":5000}}`,
		},
		{
			name:   "stream_nonzero_with_stop_and_thinking",
			opts:   &ChatOptions{Temperature: 0.7, Stop: []string{"STOP"}, EnableThinking: true, ThinkingBudget: 5000},
			stream: true,
			want:   `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"system":"rules","temperature":0.7,"stream":true,"stop_sequences":["STOP"],"thinking":{"type":"enabled","budget_tokens":5000}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				body, err = io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if tc.stream {
					_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
					return
				}
				_, _ = fmt.Fprint(w, `{"content":[{"type":"text","text":"ok"}],"model":"test","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()
			client, err := NewChatClient(ChatClientConfig{EndpointURL: server.URL, Provider: "anthropic", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			messages := []ChatMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "hi"}}
			if tc.stream {
				_, err = client.ChatStream(context.Background(), "test", messages, tc.opts, func(string) error { return nil })
			} else {
				_, err = client.Chat(context.Background(), "test", messages, tc.opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tc.want {
				t.Fatalf("wire body = %s, want exact main bytes %s", body, tc.want)
			}
		})
	}
}

func TestChatOptionsJSONBytesMatchMain(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ChatOptions
		want string
	}{
		{name: "empty", opts: ChatOptions{}, want: `{}`},
		{
			name: "full",
			opts: ChatOptions{
				MaxTokens:      512,
				Temperature:    0.7,
				Stream:         true,
				Stop:           []string{"STOP"},
				EnableThinking: true,
				ThinkingBudget: 1000,
				Tools: []ToolDefinition{{
					Name:        "lookup",
					Description: "Lookup",
					Schema:      json.RawMessage(`{"type":"object"}`),
				}},
			},
			want: `{"max_tokens":512,"temperature":0.7,"stream":true,"stop":["STOP"],"enable_thinking":true,"thinking_budget":1000,"tools":[{"name":"lookup","description":"Lookup","schema":{"type":"object"}}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("ChatOptions JSON = %s, want exact main bytes %s", got, tc.want)
			}
		})
	}
}
