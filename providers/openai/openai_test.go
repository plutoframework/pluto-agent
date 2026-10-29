package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/plutoframework/pluto-agent"
)

const finalWire = `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"summary\":\"ok\"}"}}],"usage":{"prompt_tokens":4,"completion_tokens":5}}`

func requestFixture() agent.Request {
	return agent.Request{
		Instructions: "Summarize.",
		Messages:     []agent.Message{{Role: "user", Content: "hello"}},
		OutputSchema: json.RawMessage(
			`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`,
		),
		MaxOutputTokens: 17,
	}
}

func mockProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	p, err := New(Config{
		APIKey:                "synthetic-test-credential",
		Model:                 "gpt-5.4-2026-03-05",
		HTTPClient:            server.Client(),
		Endpoint:              server.URL + "/v1/chat/completions",
		AllowInsecureLoopback: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestChatCompletionRequestContract(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" ||
			r.Header.Get(
				"Authorization",
			) != "Bearer synthetic-test-credential" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect HTTP request")
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		if body["model"] != "gpt-5.4-2026-03-05" ||
			body["max_completion_tokens"] != float64(17) ||
			body["store"] != false ||
			body["stream"] != false ||
			body["parallel_tool_calls"] != false {
			t.Errorf("incorrect options: %+v", body)
		}

		if body["reasoning_effort"] != "none" {
			t.Error("reasoning mode must be explicit")
		}

		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" ||
			format["json_schema"].(map[string]any)["strict"] != true {
			t.Error("output schema not strict")
		}

		tools := body["tools"].([]any)

		function := tools[0].(map[string]any)["function"].(map[string]any)
		if function["name"] != "lookup" || function["strict"] != true ||
			function["parameters"].(map[string]any)["additionalProperties"] != false {
			t.Error("tool schema not strict")
		}

		messages := body["messages"].([]any)
		if len(messages) != 4 ||
			messages[0].(map[string]any)["role"] != "system" ||
			messages[3].(map[string]any)["tool_call_id"] != "call-1" {
			t.Errorf("bad history: %+v", messages)
		}

		fn := messages[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
		if fn["arguments"] != `{"id":"one"}` {
			t.Error("tool arguments changed")
		}

		w.Header().Set("Content-Type", "application/json")

		_, _ = io.WriteString(w, finalWire)
	})
	req := requestFixture()
	req.Tools = []agent.ToolDefinition{
		{
			Name: "lookup",
			Schema: json.RawMessage(
				`{"type":"object","properties":{},"required":[],"additionalProperties":false}`,
			),
		},
	}
	req.Messages = append(
		req.Messages,
		agent.Message{
			Role: "assistant",
			Calls: []agent.ToolCall{
				{ID: "call-1", Name: "lookup", Arguments: `{"id":"one"}`},
			},
		},
		agent.Message{Role: "tool", CallID: "call-1", Content: `{"ok":true}`},
	)

	r, err := p.Generate(context.Background(), req)
	if err != nil || r.Finish != agent.FinishFinal ||
		string(r.Output) != `{"summary":"ok"}` ||
		r.Usage.InputTokens != 4 ||
		r.Usage.OutputTokens != 5 ||
		!r.Usage.Known {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestFunctionCallResponseContract(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = io.WriteString(
			w,
			`{"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-a","type":"function","function":{"name":"lookup","arguments":"{\"id\":\"one\"}"}}]}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`,
		)
	})

	r, err := p.Generate(context.Background(), requestFixture())
	if err != nil || r.Finish != agent.FinishTools || len(r.Calls) != 1 ||
		r.Calls[0].ID != "call-a" || r.Calls[0].Name != "lookup" || r.Calls[0].Arguments != `{"id":"one"}` {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTransportAndProtocolErrors(t *testing.T) {
	for _, test := range []struct {
		name              string
		status            int
		contentType, body string
		want              error
	}{
		{"malformed", 200, "application/json", `{`, agent.ErrProtocol},
		{"missing usage", 200, "application/json", `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}]}`, agent.ErrProtocol},
		{"unknown finish", 200, "application/json", strings.Replace(finalWire, `"stop"`, `"mystery"`, 1), agent.ErrProtocol},
		{"unknown role", 200, "application/json", strings.Replace(finalWire, `"assistant"`, `"user"`, 1), agent.ErrProtocol},
		{"negative usage", 200, "application/json", strings.Replace(finalWire, `"prompt_tokens":4`, `"prompt_tokens":-1`, 1), agent.ErrProtocol},
		{"no choices", 200, "application/json", `{"choices":[]}`, agent.ErrProtocol},
		{"wrong type", 200, "text/html", finalWire, agent.ErrProtocol},
		{"body limit", 200, "application/json", strings.Repeat("x", 2048), agent.ErrLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := mockProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)

				_, _ = io.WriteString(w, test.body)
			})
			p.maxBody = 1024

			_, err := p.Generate(context.Background(), requestFixture())
			if !errors.Is(err, test.want) {
				t.Fatalf("%v", err)
			}
		})
	}

	for _, status := range []int{400, 401, 429, 503} {
		var calls atomic.Int32

		p := mockProvider(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(status)

			_, _ = io.WriteString(w, "secret response body")
		})
		_, err := p.Generate(context.Background(), requestFixture())

		var httpError *HTTPError
		if !errors.As(err, &httpError) || httpError.StatusCode != status ||
			calls.Load() != 1 ||
			strings.Contains(err.Error(), "secret") ||
			strings.Contains(err.Error(), "credential") ||
			httpError.Retryable() != (status == 429 || status >= 500) {
			t.Fatalf("unsafe or retried error: %v", err)
		}
	}
}

func TestEndpointCredentialAndModelValidation(t *testing.T) {
	for _, c := range []Config{
		{Model: "gpt-5.4"},
		{APIKey: "x\r\ny", Model: "gpt-5.4"},
		{APIKey: "test", Model: "unknown"},
		{APIKey: "test", Model: "gpt-5.4", Endpoint: "http://example.com/v1/chat/completions", AllowInsecureLoopback: true},
		{APIKey: "test", Model: "gpt-5.4", Endpoint: "http://127.0.0.1/v1/chat/completions"},
		{APIKey: "test", Model: "gpt-5.4", Endpoint: "https://user:password@example.com"},
		{APIKey: "test", Model: "gpt-5.4", Endpoint: "https://example.com?token=secret"},
		{APIKey: "test", Model: "gpt-5.4", Endpoint: "https://example.com#fragment"},
	} {
		if _, err := New(c); err == nil {
			t.Fatalf("unsafe config accepted: model %s", c.Model)
		}
	}
}

func TestRedirectNeverForwardsCredentials(t *testing.T) {
	var destinationCalls atomic.Int32

	destination := httptest.NewServer(
		http.HandlerFunc(
			func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) },
		),
	)
	defer destination.Close()

	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	_, err := p.Generate(context.Background(), requestFixture())

	var httpError *HTTPError
	if !errors.As(err, &httpError) || httpError.StatusCode != 307 ||
		destinationCalls.Load() != 0 {
		t.Fatalf("redirect leaked: %v", err)
	}
}

func TestContextCancelsHTTP(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	_, err := p.Generate(ctx, requestFixture())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
}
