package agent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestRegistrationFailsClosed(t *testing.T) {
	for name, change := range map[string]func(*agent.ToolConfig[deps, args, output]){
		"undeclared":            func(c *agent.ToolConfig[deps, args, output]) { c.Access = agent.Access{} },
		"ambiguous":             func(c *agent.ToolConfig[deps, args, output]) { c.Access.Public = true },
		"no resource policy":    func(c *agent.ToolConfig[deps, args, output]) { c.Policy = nil },
		"no effect declaration": func(c *agent.ToolConfig[deps, args, output]) { c.Effect = "" },
		"bad name":              func(c *agent.ToolConfig[deps, args, output]) { c.Name = "bad/name" },
		"no handler":            func(c *agent.ToolConfig[deps, args, output]) { c.Handle = nil },
	} {
		t.Run(name, func(t *testing.T) {
			c := toolConfig()
			change(&c)

			if _, err := agent.NewTool(c); !errors.Is(err, agent.ErrConfig) {
				t.Fatalf("registration accepted: %v", err)
			}
		})
	}
}

func TestEveryAuthorizationGateDeniesBeforeHandler(t *testing.T) {
	for _, test := range []struct {
		name        string
		tool        string
		raw         string
		restriction agent.Restriction
		change      func(*agent.Config[deps, output])
	}{
		{name: "out of agent", tool: "pay", raw: validArgs},
		{name: "empty restriction", tool: "lookup", raw: validArgs, restriction: agent.OnlyTools()},
		{name: "unknown run ID", tool: "lookup", raw: validArgs, restriction: agent.OnlyTools("pay")},
		{name: "permission", tool: "lookup", raw: validArgs, change: func(c *agent.Config[deps, output]) {
			c.Authorizer = agent.AuthorizerFunc(func(context.Context, agent.Actor, string) error { return errors.New("revoked") })
		}},
		{name: "resource", tool: "lookup", raw: `{"resource":"other","count":1,"mode":"read"}`},
		{name: "verified principal", tool: "lookup", raw: validArgs, change: func(c *agent.Config[deps, output]) {
			c.Principal = func(deps) (agent.Actor, error) { return agent.Actor{}, errors.New("unverified") }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, run := fixture(
				t,
				[]scripted.Step{
					scripted.Calls(call("denied", test.tool, test.raw)),
				},
				nil,
				test.change,
			)
			run.Tools = test.restriction

			r, err := a.Run(context.Background(), run)
			if !errors.Is(err, agent.ErrDenied) ||
				r.Status != agent.StatusFailed ||
				run.Deps.calls.Load() != 0 {
				t.Fatalf(
					"denial failed: %+v %v calls=%d",
					r,
					err,
					run.Deps.calls.Load(),
				)
			}
		})
	}
}

func TestPublicToolStillHasResourceAndRunGates(t *testing.T) {
	for _, test := range []struct {
		name, raw   string
		restriction agent.Restriction
		want        error
	}{
		{name: "explicit public", raw: validArgs},
		{name: "public restricted", raw: validArgs, restriction: agent.OnlyTools(), want: agent.ErrDenied},
		{name: "public resource", raw: `{"resource":"other","count":1,"mode":"read"}`, want: agent.ErrDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, run := fixture(t, []scripted.Step{
				scripted.Calls(
					call("public", "lookup", test.raw),
				),
				scripted.Final(`{"summary":"ok"}`),
			}, func(c *agent.ToolConfig[deps, args, output]) { c.Access = agent.Access{Public: true} },
				func(c *agent.Config[deps, output]) { c.Authorizer = nil })
			run.Tools = test.restriction
			_, err := a.Run(context.Background(), run)

			wantCalls := int32(1)
			if test.want != nil {
				wantCalls = 0
			}

			if !errors.Is(err, test.want) ||
				run.Deps.calls.Load() != wantCalls {
				t.Fatalf("%v calls %d", err, run.Deps.calls.Load())
			}
		})
	}
}

func TestRepeatedReadChecksRevocationEveryTime(t *testing.T) {
	var (
		revoked atomic.Bool
		checks  atomic.Int32
	)

	second := scripted.Calls(call("second", "lookup", validArgs))

	second.Check = func(agent.Request) error {
		revoked.Store(true)

		return nil
	}
	a, run := fixture(
		t,
		[]scripted.Step{
			scripted.Calls(call("first", "lookup", validArgs)),
			second,
		},
		nil,
		func(c *agent.Config[deps, output]) {
			c.Authorizer = agent.AuthorizerFunc(
				func(context.Context, agent.Actor, string) error {
					checks.Add(1)

					if revoked.Load() {
						return agent.ErrDenied
					}

					return nil
				},
			)
		},
	)

	_, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrDenied) || checks.Load() != 2 ||
		run.Deps.calls.Load() != 1 {
		t.Fatalf(
			"revocation bypass: %v checks=%d calls=%d",
			err,
			checks.Load(),
			run.Deps.calls.Load(),
		)
	}
}

func TestRunRestrictionFiltersSchemasWithoutWidening(t *testing.T) {
	step := scripted.Calls(call("hidden", "lookup", validArgs))
	step.Check = func(req agent.Request) error {
		if len(req.Tools) != 0 {
			return errors.New("restricted schema leaked")
		}

		return nil
	}
	a, run := fixture(t, []scripted.Step{step}, nil, nil)
	run.Tools = agent.OnlyTools()

	_, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrDenied) || run.Deps.calls.Load() != 0 {
		t.Fatalf("%v", err)
	}
}

func TestPermissionDenialSkipsCallerValidators(t *testing.T) {
	var validations atomic.Int32

	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("denied", "lookup", validArgs))},
		func(c *agent.ToolConfig[deps, args, output]) {
			c.Validate = func(context.Context, deps, args) error {
				validations.Add(1)

				return nil
			}
		},
		func(c *agent.Config[deps, output]) {
			c.Authorizer = agent.AuthorizerFunc(
				func(context.Context, agent.Actor, string) error { return agent.ErrDenied },
			)
		},
	)

	_, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrDenied) || validations.Load() != 0 ||
		run.Deps.calls.Load() != 0 {
		t.Fatalf("permission denial reached application callback: %v", err)
	}
}

func TestPublicApprovalStillRequiresVerifiedSubject(t *testing.T) {
	var verifications atomic.Int32

	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("public", "lookup", validArgs))},
		func(c *agent.ToolConfig[deps, args, output]) {
			c.Access = agent.Access{Public: true}
			c.ApprovalRequired = true
			c.Policy = func(context.Context, deps, agent.Actor, args) error { return nil }
		},
		func(c *agent.Config[deps, output]) {
			c.Authorizer = nil
			c.Approvals = approvalFunc(
				func(context.Context, agent.ApprovalRequest) error {
					verifications.Add(1)

					return nil
				},
			)
		},
	)
	run.Deps.actor = agent.Actor{}

	_, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrApproval) || verifications.Load() != 0 ||
		run.Deps.calls.Load() != 0 {
		t.Fatalf("anonymous approval accepted: %v", err)
	}
}

type approvalFunc func(context.Context, agent.ApprovalRequest) error

func (f approvalFunc) Verify(
	ctx context.Context,
	r agent.ApprovalRequest,
) error {
	return f(ctx, r)
}

func TestApprovalIsExactAndAdditional(t *testing.T) {
	for _, mismatch := range []string{"none", "missing", "run", "tool", "actor", "scope", "args", "permission"} {
		t.Run(mismatch, func(t *testing.T) {
			verified := 0
			a, run := fixture(t, []scripted.Step{
				scripted.Calls(
					call("approval", "lookup", validArgs),
				),
				scripted.Final(`{"summary":"ok"}`),
			}, func(c *agent.ToolConfig[deps, args, output]) { c.ApprovalRequired = true },
				func(c *agent.Config[deps, output]) {
					if mismatch == "missing" {
						return
					}

					if mismatch == "permission" {
						c.Authorizer = agent.AuthorizerFunc(
							func(context.Context, agent.Actor, string) error { return agent.ErrDenied },
						)
					}

					c.Approvals = approvalFunc(
						func(_ context.Context, r agent.ApprovalRequest) error {
							verified++

							// Simulate a persisted host-approved record and compare every binding.
							wantRun, wantTool := "run-1", "lookup"
							wantActor := agent.Actor{
								SubjectID: "verified",
								Scope:     "north",
							}
							wantArgs := validArgs

							switch mismatch {
							case "run":
								wantRun = "other"
							case "tool":
								wantTool = "other"
							case "actor":
								wantActor.SubjectID = "other"
							case "scope":
								wantActor.Scope = "other"
							case "args":
								wantArgs = `{"resource":"one","count":2,"mode":"read"}`
							}

							if r.RunID != wantRun || r.ToolID != wantTool ||
								r.Actor != wantActor ||
								string(r.Arguments) != wantArgs ||
								len(r.Hash) != 64 {
								return agent.ErrApproval
							}

							// A verifier mutating its copy cannot change dispatched arguments.
							r.Arguments[0] = '!'

							return nil
						},
					)
				})

			_, err := a.Run(context.Background(), run)
			if mismatch == "none" {
				if err != nil || run.Deps.calls.Load() != 1 || verified != 1 {
					t.Fatalf("%v", err)
				}
			} else {
				want := agent.ErrApproval
				if mismatch == "permission" {
					want = agent.ErrDenied
				}

				if !errors.Is(err, want) || run.Deps.calls.Load() != 0 {
					t.Fatalf("%v calls=%d", err, run.Deps.calls.Load())
				}

				if (mismatch == "missing" || mismatch == "permission") &&
					verified != 0 {
					t.Fatal("approval bypassed permission")
				}
			}
		})
	}
}

func TestSideEffectsAndCallIDsNotReplayed(t *testing.T) {
	for _, test := range []struct {
		name, secondID string
		effect         agent.Effect
		expected       int32
	}{
		{"write duplicate args", "second", agent.SideEffect, 1},
		{"duplicate call ID", "first", agent.ReadOnly, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, run := fixture(t, []scripted.Step{
				scripted.Calls(
					call("first", "lookup", validArgs),
				),
				scripted.Calls(call(test.secondID, "lookup", validArgs)),
			}, func(c *agent.ToolConfig[deps, args, output]) { c.Effect = test.effect }, nil)

			_, err := a.Run(context.Background(), run)
			if !errors.Is(err, agent.ErrReplay) ||
				run.Deps.calls.Load() != test.expected {
				t.Fatalf("%v calls %d", err, run.Deps.calls.Load())
			}
		})
	}
}
