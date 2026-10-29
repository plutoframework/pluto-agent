package agent_test

import (
	"context"
	"sync/atomic"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func FuzzDispatchDenial(f *testing.F) {
	f.Add(validArgs, uint8(0))
	f.Add(`{"resource":"other","count":1,"mode":"read"}`, uint8(2))
	f.Add(`{"resource":"one","count":0,"mode":"write"}`, uint8(3))
	f.Fuzz(func(t *testing.T, raw string, gate uint8) {
		if len(raw) > 8192 {
			t.Skip()
		}

		// Every fuzz case closes at least one independent authorization gate.
		denied := gate % 4

		name := "lookup"
		if denied == 0 {
			name = "unknown"
		}

		a, run := fixture(
			t,
			[]scripted.Step{scripted.Calls(call("fuzz", name, raw))},
			func(c *agent.ToolConfig[deps, args, output]) {
				if denied == 3 {
					c.Policy = func(context.Context, deps, agent.Actor, args) error { return agent.ErrDenied }
				}
			},
			func(c *agent.Config[deps, output]) {
				c.Limits = agent.DefaultLimits()

				c.Limits.MaxRepairs = 0
				if denied == 2 {
					c.Authorizer = agent.AuthorizerFunc(
						func(context.Context, agent.Actor, string) error { return agent.ErrDenied },
					)
				}
			},
		)
		if denied == 1 {
			run.Tools = agent.OnlyTools()
		}

		run.Deps.calls = &atomic.Int32{}

		result, err := a.Run(context.Background(), run)
		if err == nil || result.Status == agent.StatusCompleted ||
			run.Deps.calls.Load() != 0 {
			t.Fatal("denied handler executed")
		}
	})
}
