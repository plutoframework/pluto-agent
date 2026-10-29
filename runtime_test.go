package agent_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

type deps struct {
	actor agent.Actor
	calls *atomic.Int32
}

type args struct {
	Resource string `json:"resource"`
	Count    int    `json:"count"`
	Mode     string `json:"mode"     enum:"read,write"`
}

type output struct {
	Summary string `json:"summary"`
}

const validArgs = `{"resource":"one","count":1,"mode":"read"}`

func call(id, name, raw string) agent.ToolCall {
	return agent.ToolCall{ID: id, Name: name, Arguments: raw}
}

func toolConfig() agent.ToolConfig[deps, args, output] {
	return agent.ToolConfig[deps, args, output]{
		Name:   "lookup",
		Access: agent.Access{Permission: "records:read"},
		Effect: agent.ReadOnly,
		Validate: func(_ context.Context, _ deps, a args) error {
			if a.Count < 1 || a.Count > 10 {
				return errors.New("invalid count")
			}

			return nil
		},
		Policy: func(_ context.Context, _ deps, actor agent.Actor, a args) error {
			if actor.Scope != "north" || a.Resource != "one" {
				return agent.ErrDenied
			}

			return nil
		},
		Handle: func(_ context.Context, d deps, _ args) (output, error) {
			d.calls.Add(1)

			return output{Summary: "found"}, nil
		},
	}
}

func fixture(
	t *testing.T,
	steps []scripted.Step,
	changeTool func(*agent.ToolConfig[deps, args, output]),
	changeAgent func(*agent.Config[deps, output]),
) (*agent.Agent[deps, output], agent.Run[deps]) {
	t.Helper()

	tc := toolConfig()
	if changeTool != nil {
		changeTool(&tc)
	}

	tool, err := agent.NewTool(tc)
	if err != nil {
		t.Fatal(err)
	}

	cfg := agent.Config[deps, output]{
		Provider:  scripted.New(steps...),
		Tools:     []agent.Tool[deps]{tool},
		Principal: func(d deps) (agent.Actor, error) { return d.actor, nil },
		Authorizer: agent.AuthorizerFunc(
			func(_ context.Context, actor agent.Actor, permission string) error {
				if actor != (agent.Actor{SubjectID: "verified", Scope: "north"}) ||
					permission != "records:read" {
					return agent.ErrDenied
				}

				return nil
			},
		),
	}
	if changeAgent != nil {
		changeAgent(&cfg)
	}

	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	run := agent.Run[deps]{
		ID:    "run-1",
		Input: "review",
		Deps: deps{
			actor: agent.Actor{SubjectID: "verified", Scope: "north"},
			calls: &atomic.Int32{},
		},
	}

	return a, run
}

func TestTypedRunHistoryAndOutputRepair(t *testing.T) {
	checked := false
	final := scripted.Final(`{"summary":"complete"}`)
	final.Check = func(req agent.Request) error {
		checked = true

		if len(req.Messages) != 5 || req.Messages[2].CallID != "first" ||
			req.Messages[2].Content != `{"summary":"found"}` {
			return fmt.Errorf(
				"tool history missing or corrupted: %+v",
				req.Messages,
			)
		}

		return nil
	}
	a, run := fixture(t, []scripted.Step{
		scripted.Calls(call("first", "lookup", validArgs)),
		scripted.Final(`{"summary":7}`), final,
	}, nil, nil)

	r, err := a.Run(context.Background(), run)
	if err != nil ||
		r.Status != agent.StatusCompleted ||
		r.Output.Summary != "complete" ||
		r.ModelCalls != 3 || r.ToolCalls != 1 ||
		r.Repairs != 1 ||
		run.Deps.calls.Load() != 1 ||
		!checked {
		t.Fatalf(
			"unexpected run: %+v, %v, calls=%d",
			r,
			err,
			run.Deps.calls.Load(),
		)
	}
}

func TestMalformedArgumentsRepairBeforeHandler(t *testing.T) {
	for _, raw := range []string{
		`{`, `{}`, `{"resource":"one","count":1,"mode":"unknown"}`,
		`{"resource":"one","count":0,"mode":"read"}`,
		`{"resource":"one","count":1,"mode":"read","actor":"admin"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			a, run := fixture(t, []scripted.Step{
				scripted.Calls(call("bad", "lookup", raw)),
				scripted.Calls(call("good", "lookup", validArgs)),
				scripted.Final(`{"summary":"ok"}`),
			}, nil, nil)

			r, err := a.Run(context.Background(), run)
			if err != nil || r.Repairs != 1 || run.Deps.calls.Load() != 1 {
				t.Fatalf(
					"repair: %+v %v, calls %d",
					r,
					err,
					run.Deps.calls.Load(),
				)
			}
		})
	}
}

func TestMalformedFinalRepair(t *testing.T) {
	a, run := fixture(
		t,
		[]scripted.Step{
			scripted.Final(`{`),
			scripted.Final(`{"summary":"ok"}`),
		},
		nil,
		nil,
	)

	r, err := a.Run(context.Background(), run)
	if err != nil || r.Repairs != 1 || r.Output.Summary != "ok" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestHandlerFailureNeverRetried(t *testing.T) {
	want := errors.New("external result uncertain")
	a, run := fixture(t, []scripted.Step{
		scripted.Calls(call("write", "lookup", validArgs)),
		scripted.Final(`{"summary":"must not run"}`),
	}, func(c *agent.ToolConfig[deps, args, output]) {
		c.Effect = agent.SideEffect
		c.Handle = func(_ context.Context, d deps, _ args) (output, error) {
			d.calls.Add(1)

			return output{}, want
		}
	}, nil)

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, want) || r.ModelCalls != 1 || r.Repairs != 0 ||
		run.Deps.calls.Load() != 1 {
		t.Fatalf("handler retried: %+v %v", r, err)
	}
}

func TestProviderFailureAfterWriteDoesNotReplay(t *testing.T) {
	want := errors.New("transport broke")
	a, run := fixture(t, []scripted.Step{
		scripted.Calls(call("write", "lookup", validArgs)), {Err: want},
	}, func(c *agent.ToolConfig[deps, args, output]) { c.Effect = agent.SideEffect }, nil)

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, want) || !errors.Is(err, agent.ErrProvider) ||
		r.ModelCalls != 2 || r.Repairs != 0 || run.Deps.calls.Load() != 1 {
		t.Fatalf("provider retried: %+v %v", r, err)
	}
}

func TestBusinessOutputValidator(t *testing.T) {
	a, run := fixture(
		t,
		[]scripted.Step{
			scripted.Final(`{"summary":""}`),
			scripted.Final(`{"summary":"ok"}`),
		},
		nil,
		func(c *agent.Config[deps, output]) {
			c.ValidateOutput = func(_ context.Context, _ deps, o output) error {
				if o.Summary == "" {
					return errors.New("empty")
				}

				return nil
			}
		},
	)

	r, err := a.Run(context.Background(), run)
	if err != nil || r.Repairs != 1 || r.Output.Summary != "ok" {
		t.Fatalf("%+v %v", r, err)
	}
}
