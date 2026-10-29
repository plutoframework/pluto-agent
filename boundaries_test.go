package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestFinalRepairDoesNotRepeatSideEffect(t *testing.T) {
	a, run := fixture(t, []scripted.Step{
		scripted.Calls(call("write", "lookup", validArgs)),
		scripted.Final(`{"summary":false}`),
		scripted.Final(`{"summary":"ok"}`),
	}, func(c *agent.ToolConfig[deps, args, output]) { c.Effect = agent.SideEffect }, nil)

	r, err := a.Run(context.Background(), run)
	if err != nil || r.Repairs != 1 || r.ModelCalls != 3 ||
		run.Deps.calls.Load() != 1 {
		t.Fatalf("side effect replayed on final repair: %+v %v", r, err)
	}
}

func TestPolicyAndApprovalCannotMutateHandlerArguments(t *testing.T) {
	type listArgs struct {
		IDs []string `json:"ids"`
	}

	type listOutput struct {
		Items []string `json:"items"`
	}

	const canonical = `{"ids":["safe"]}`

	tool, err := agent.NewTool(agent.ToolConfig[struct{}, listArgs, listOutput]{
		Name:             "list",
		Access:           agent.Access{Public: true},
		Effect:           agent.ReadOnly,
		ApprovalRequired: true,
		Policy: func(_ context.Context, _ struct{}, _ agent.Actor, a listArgs) error {
			a.IDs[0] = "changed by policy"
			return nil
		},
		Handle: func(_ context.Context, _ struct{}, a listArgs) (listOutput, error) {
			if a.IDs[0] != "safe" {
				return listOutput{}, errors.New(
					"policy changed dispatched arguments",
				)
			}

			return listOutput{Items: a.IDs}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := agent.New(agent.Config[struct{}, listOutput]{
		Provider: scripted.New(
			scripted.Calls(call("list-call", "list", ` { "ids" : ["safe"] } `)),
			scripted.Final(`{"items":["safe"]}`),
		),
		Tools: []agent.Tool[struct{}]{tool},
		Principal: func(struct{}) (agent.Actor, error) {
			return agent.Actor{SubjectID: "verified", Scope: "north"}, nil
		},
		Approvals: approvalFunc(
			func(_ context.Context, r agent.ApprovalRequest) error {
				digest := sha256.Sum256([]byte(canonical))
				if string(r.Arguments) != canonical ||
					r.Hash != hex.EncodeToString(digest[:]) {
					return agent.ErrApproval
				}

				r.Arguments[0] = '!'

				return nil
			},
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := a.RunStream(
		context.Background(),
		agent.Run[struct{}]{ID: "list-run"},
		func(e agent.Event[listOutput]) error {
			if e.Kind == agent.EventFinal {
				e.Output.Items[0] = "changed by consumer"
			}

			return nil
		},
	)
	if err != nil || r.Output.Items[0] != "safe" {
		t.Fatalf("shared output/args: %+v %v", r, err)
	}
}

func TestInvalidToolResultStopsWithoutRetry(t *testing.T) {
	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("one", "lookup", validArgs))},
		func(c *agent.ToolConfig[deps, args, output]) {
			c.ValidateResult = func(context.Context, deps, output) error { return errors.New("rejected tool output") }
		},
		nil,
	)

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, agent.ErrValidation) || r.Repairs != 0 ||
		run.Deps.calls.Load() != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}
