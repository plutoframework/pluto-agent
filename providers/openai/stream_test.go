package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
)

const textSSE = "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"{\\\"summary\\\":\"}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\\\"ok\\\"}\"}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":5}}\n\n" +
	"data: [DONE]\n\n"

const toolsSSE = `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"id\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"one\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":5}}

data: [DONE]

`

func TestNativeStreamingHTTPContract(t *testing.T) {
	p := mockProvider(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"stream":true`) ||
			!strings.Contains(string(raw), `"include_usage":true`) {
			t.Error("stream usage not requested")
		}

		w.Header().Set("Content-Type", "text/event-stream")

		_, _ = io.WriteString(w, textSSE)
	})

	var partials []string

	r, err := p.Stream(
		context.Background(),
		requestFixture(),
		func(d agent.Delta) error {
			partials = append(partials, d.Text)

			return nil
		},
	)
	if err != nil || r.Finish != agent.FinishFinal ||
		string(r.Output) != `{"summary":"ok"}` ||
		strings.Join(partials, "") != string(r.Output) ||
		r.Usage.OutputTokens != 5 ||
		len(partials) != 2 {
		t.Fatalf("%+v %v partials=%v", r, err, partials)
	}
}

func TestNativeStreamingFunctionArguments(t *testing.T) {
	p := &Provider{maxBody: 4096, maxOutput: 1024}

	r, err := p.decodeStream(
		context.Background(),
		strings.NewReader(toolsSSE),
		func(agent.Delta) error {
			t.Error("tool args exposed as text")

			return nil
		},
	)
	if err != nil || r.Finish != agent.FinishTools || len(r.Calls) != 1 ||
		r.Calls[0].ID != "call-1" ||
		r.Calls[0].Arguments != `{"id":"one"}` {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestStreamTruncationAndLimits(t *testing.T) {
	for name, raw := range map[string]string{
		"disconnect":       strings.Replace(textSSE, "data: [DONE]\n\n", "", 1),
		"missing usage":    strings.Replace(textSSE, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":5}}\n\n", "", 1),
		"malformed":        "data: {\n\ndata: [DONE]\n\n",
		"done early":       "data: [DONE]\n\n",
		"unknown field":    "unknown: x\n\n",
		"unknown finish":   strings.Replace(textSSE, "stop", "mystery", 1),
		"negative index":   strings.Replace(toolsSSE, `"index":0,"id"`, `"index":-1,"id"`, 1),
		"sparse index":     strings.Replace(toolsSSE, `"index":0,"id"`, `"index":127,"id"`, 1),
		"duplicate finish": strings.Replace(textSSE, "data: [DONE]", `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]", 1),
	} {
		t.Run(name, func(t *testing.T) {
			p := &Provider{maxBody: 4096, maxOutput: 1024}

			_, err := p.decodeStream(
				context.Background(),
				strings.NewReader(raw),
				func(agent.Delta) error { return nil },
			)
			if err == nil {
				t.Fatal("invalid stream accepted")
			}
		})
	}

	for _, p := range []*Provider{{maxBody: 20, maxOutput: 1024}, {maxBody: 4096, maxOutput: 2}} {
		_, err := p.decodeStream(
			context.Background(),
			strings.NewReader(textSSE),
			func(agent.Delta) error { return nil },
		)
		if !errors.Is(err, agent.ErrLimit) &&
			!errors.Is(err, agent.ErrProtocol) {
			t.Fatalf("limit: %v", err)
		}
	}
}

func TestStreamEmitterErrorsAndCancellation(t *testing.T) {
	p := &Provider{maxBody: 4096, maxOutput: 1024}
	want := errors.New("consumer left")

	_, err := p.decodeStream(
		context.Background(),
		strings.NewReader(textSSE),
		func(agent.Delta) error { return want },
	)
	if !errors.Is(err, want) {
		t.Fatalf("%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = p.decodeStream(
		ctx,
		strings.NewReader(textSSE),
		func(agent.Delta) error {
			t.Fatal("emitted after cancellation")

			return nil
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func FuzzStreamDecode(f *testing.F) {
	f.Add(textSSE)
	f.Add(toolsSSE)
	f.Add("data: [DONE]\n\n")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 16384 {
			t.Skip()
		}

		p := &Provider{maxBody: 8192, maxOutput: 2048}
		size := 0

		r, err := p.decodeStream(
			context.Background(),
			strings.NewReader(raw),
			func(d agent.Delta) error {
				size += len(d.Text)
				if size > 2048 {
					t.Fatal("unbounded stream emission")
				}

				return nil
			},
		)
		if err == nil &&
			(!r.Usage.Known || (r.Finish != agent.FinishFinal && r.Finish != agent.FinishTools && r.Finish != agent.FinishLength && r.Finish != agent.FinishRefusal)) {
			t.Fatal("unvalidated completion metadata")
		}
	})
}
