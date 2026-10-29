package agent_test

import (
	"context"
	"errors"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestRuntimeLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits func(*agent.Limits)
		steps  []scripted.Step
		want   error
		calls  int32
	}{
		{name: "input", limits: func(l *agent.Limits) { l.MaxInputBytes = 2 }, steps: []scripted.Step{scripted.Final(`{"summary":"ok"}`)}, want: agent.ErrLimit},
		{name: "request", limits: func(l *agent.Limits) { l.MaxRequestBytes = 2 }, steps: []scripted.Step{scripted.Final(`{"summary":"ok"}`)}, want: agent.ErrLimit},
		{name: "model output", limits: func(l *agent.Limits) { l.MaxModelOutputBytes = 2 }, steps: []scripted.Step{scripted.Final(`{"summary":"ok"}`)}, want: agent.ErrLimit},
		{name: "tool output", limits: func(l *agent.Limits) { l.MaxToolOutputBytes = 2 }, steps: []scripted.Step{scripted.Calls(call("1", "lookup", validArgs))}, want: agent.ErrLimit, calls: 1},
		{name: "model calls", limits: func(l *agent.Limits) { l.MaxModelCalls = 1 }, steps: []scripted.Step{scripted.Calls(call("1", "lookup", validArgs)), scripted.Final(`{"summary":"ok"}`)}, want: agent.ErrLimit, calls: 1},
		{name: "tool calls batch", limits: func(l *agent.Limits) { l.MaxToolCalls = 1 }, steps: []scripted.Step{scripted.Calls(call("1", "lookup", validArgs), call("2", "lookup", validArgs))}, want: agent.ErrLimit},
		{name: "repairs", limits: func(l *agent.Limits) { l.MaxRepairs = 1 }, steps: []scripted.Step{scripted.Final(`{}`), scripted.Final(`{}`), scripted.Final(`{"summary":"unused"}`)}, want: agent.ErrValidation},
		{name: "no repairs", limits: func(l *agent.Limits) { l.MaxRepairs = 0 }, steps: []scripted.Step{scripted.Calls(call("bad", "lookup", `{}`))}, want: agent.ErrValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, run := fixture(
				t,
				test.steps,
				nil,
				func(c *agent.Config[deps, output]) {
					c.Limits = agent.DefaultLimits()
					test.limits(&c.Limits)
				},
			)

			r, err := a.Run(context.Background(), run)
			if !errors.Is(err, test.want) ||
				run.Deps.calls.Load() != test.calls ||
				r.Status == agent.StatusCompleted {
				t.Fatalf(
					"limit failed: %+v %v calls=%d",
					r,
					err,
					run.Deps.calls.Load(),
				)
			}
		})
	}
}

func TestUsageIsPostHocAndOutputRequestIsBounded(t *testing.T) {
	step := scripted.Final(`{"summary":"ok"}`)
	step.Response.Usage = agent.Usage{
		Known:        true,
		InputTokens:  8,
		OutputTokens: 3,
	}
	step.Check = func(r agent.Request) error {
		if r.MaxOutputTokens != 10 {
			return errors.New("output budget not forwarded")
		}

		return nil
	}
	a, run := fixture(
		t,
		[]scripted.Step{step},
		nil,
		func(c *agent.Config[deps, output]) {
			c.Limits = agent.DefaultLimits()
			c.Limits.MaxTokens = 10
		},
	)

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrLimit) || r.Status != agent.StatusLimited ||
		r.Usage.InputTokens != 8 ||
		r.Usage.OutputTokens != 3 {
		t.Fatalf("post-hoc accounting: %+v %v", r, err)
	}
}

func TestCumulativeToolLimitAcrossModelTurns(t *testing.T) {
	a, run := fixture(t, []scripted.Step{
		scripted.Calls(call("first", "lookup", validArgs)),
		scripted.Calls(call("second", "lookup", validArgs)),
	}, nil, func(c *agent.Config[deps, output]) {
		c.Limits = agent.DefaultLimits()
		c.Limits.MaxToolCalls = 1
	})

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrLimit) || r.Status != agent.StatusLimited ||
		run.Deps.calls.Load() != 1 || r.ToolCalls != 1 {
		t.Fatalf("cumulative tool limit failed: %+v %v", r, err)
	}
}

func TestMissingUsageIsNotZero(t *testing.T) {
	step := scripted.Final(`{"summary":"ok"}`)
	step.Response.Usage.Known = false
	a, run := fixture(t, []scripted.Step{step}, nil, nil)

	_, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrProtocol) {
		t.Fatalf("%v", err)
	}
}

type capabilityProvider struct{ caps agent.Capabilities }

func (p capabilityProvider) Capabilities() agent.Capabilities { return p.caps }

func (capabilityProvider) Generate(
	context.Context,
	agent.Request,
) (agent.Response, error) {
	return agent.Response{}, errors.New("must not call")
}

func TestCapabilitiesAndConfigurationFailExplicitly(t *testing.T) {
	for _, caps := range []agent.Capabilities{
		{Tools: true, Usage: true},
		{Tools: true, StructuredOutput: true},
		{StructuredOutput: true, Usage: true},
	} {
		tool, err := agent.NewTool(toolConfig())
		if err != nil {
			t.Fatal(err)
		}

		_, err = agent.New(agent.Config[deps, output]{
			Provider: capabilityProvider{
				caps: caps,
			},
			Tools:     []agent.Tool[deps]{tool},
			Principal: func(d deps) (agent.Actor, error) { return d.actor, nil },
		})
		if !errors.Is(err, agent.ErrCapability) {
			t.Fatalf("%v", err)
		}
	}

	a, run := fixture(t, nil, nil, func(c *agent.Config[deps, output]) {
		c.Provider = capabilityProvider{
			caps: agent.Capabilities{
				Tools:            true,
				StructuredOutput: true,
				Usage:            true,
				Streaming:        true,
			},
		}
	})

	_, err := a.RunStream(
		context.Background(),
		run,
		func(agent.Event[output]) error { return nil },
	)
	if !errors.Is(err, agent.ErrCapability) {
		t.Fatalf("silent streaming fallback: %v", err)
	}
}

func TestProtocolAndBatchErrorsBeforeEffects(t *testing.T) {
	for _, response := range []agent.Response{
		{Finish: agent.FinishFinal, Output: []byte(`{"summary":"ok"}`), Calls: []agent.ToolCall{call("a", "lookup", validArgs)}},
		{Finish: agent.FinishTools},
		{Finish: agent.FinishTools, Calls: []agent.ToolCall{call("same", "lookup", validArgs), call("same", "lookup", validArgs)}},
		{Finish: agent.FinishTools, Calls: []agent.ToolCall{call("good", "lookup", validArgs), call("bad", "unknown", validArgs)}},
		{Finish: "unsupported"},
	} {
		response.Usage.Known = true
		a, run := fixture(t, []scripted.Step{{Response: response}}, nil, nil)

		_, err := a.Run(context.Background(), run)
		if err == nil || run.Deps.calls.Load() != 0 {
			t.Fatalf("protocol ran handler: %v", err)
		}
	}
}
