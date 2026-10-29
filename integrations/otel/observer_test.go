package otel_test

import (
	"context"
	"math"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	agentotel "github.com/plutoframework/pluto-agent/integrations/otel"
	"github.com/plutoframework/pluto-agent/providers/scripted"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type empty struct{}

type result struct {
	Summary string `json:"summary"`
}

func TestRuntimeSpansHaveNoSensitivePayloads(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	observer, err := agentotel.New(provider.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}

	tool, err := agent.NewTool(agent.ToolConfig[empty, empty, result]{
		Name:   "sensitive_tool",
		Access: agent.Access{Public: true},
		Effect: agent.ReadOnly,
		Policy: func(context.Context, empty, agent.Actor, empty) error { return nil },
		Handle: func(context.Context, empty, empty) (result, error) {
			return result{Summary: "secret tool payload"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	final := scripted.Final(`{"summary":"secret output"}`)
	final.Response.Usage = agent.Usage{
		Known:        true,
		InputTokens:  3,
		OutputTokens: 2,
	}

	a, err := agent.New(agent.Config[empty, result]{
		Provider: scripted.New(
			scripted.Calls(
				agent.ToolCall{
					ID:        "call",
					Name:      "sensitive_tool",
					Arguments: `{}`,
				},
			),
			final,
		),
		Tools:    []agent.Tool[empty]{tool},
		Observer: observer,
		Principal: func(empty) (agent.Actor, error) {
			return agent.Actor{
				SubjectID: "secret-identity",
				Scope:     "secret-tenant",
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(
		context.Background(),
		agent.Run[empty]{ID: "secret-run-id", Input: "secret prompt"},
	)
	if err != nil {
		t.Fatal(err)
	}

	spans := recorder.Ended()
	if len(spans) != 4 {
		t.Fatalf("missing run/model/tool spans: %d", len(spans))
	}

	runSpan := spans[3]
	if runSpan.Name() != "agent.run" {
		t.Fatal("run span missing")
	}

	counts := make(map[string]int)
	for _, span := range spans {
		counts[span.Name()]++
		if span.Name() != "agent.run" &&
			span.Parent().SpanID() != runSpan.SpanContext().SpanID() {
			t.Fatal("span not parented to run")
		}

		if len(span.Events()) != 0 {
			t.Fatal("payload events recorded")
		}

		for _, attr := range span.Attributes() {
			switch string(attr.Key) {
			case "agent.failed":
				if attr.Value.AsBool() {
					t.Fatal("unexpected failure")
				}
			case "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens":
				if attr.Value.AsInt64() < 0 || attr.Value.AsInt64() > 5 {
					t.Fatal("unexpected token count")
				}
			default:
				t.Fatalf("unexpected attribute %s", attr.Key)
			}
		}
	}

	if counts["agent.model"] != 2 || counts["agent.tool"] != 1 ||
		counts["agent.run"] != 1 {
		t.Fatalf("%v", counts)
	}
}

func TestObserverBoundsUsageNamesAndDoesNotRecordErrorText(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()

	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(context.Background())

	o, err := agentotel.New(provider.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}

	_, end := o.Start(context.Background(), agent.OperationModel)
	end(
		agent.Observation{
			Failed: true,
			Usage: agent.Usage{
				Known:        true,
				InputTokens:  math.MaxInt64,
				OutputTokens: -1,
			},
		},
	)
	end(agent.Observation{})

	_, unknown := o.Start(
		context.Background(),
		agent.Operation("secret-tool-name"),
	)
	unknown(agent.Observation{})

	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Status().Code != codes.Error ||
		spans[1].Name() != "agent.other" {
		t.Fatal("unbounded span or repeated end")
	}

	for _, attr := range spans[0].Attributes() {
		if string(attr.Key) == "gen_ai.usage.input_tokens" &&
			attr.Value.AsInt64() != 1_000_000_000 {
			t.Fatal("usage not bounded")
		}

		if string(attr.Key) == "gen_ai.usage.output_tokens" &&
			attr.Value.AsInt64() != 0 {
			t.Fatal("negative usage")
		}
	}
}
