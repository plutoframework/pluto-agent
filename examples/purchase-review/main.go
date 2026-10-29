// Purchase review is a synthetic read-only web-feature core, independent of
// HTTP request decoding. Replace the scripted provider with OpenAI at host
// composition time; business tools and authorization remain the same.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func build(
	provider agent.Provider,
) (*agent.Agent[Dependencies, Review], error) {
	authorizer, err := permissions()
	if err != nil {
		return nil, err
	}

	tools, err := reviewTools()
	if err != nil {
		return nil, err
	}

	return agent.New(agent.Config[Dependencies, Review]{
		Provider:     provider,
		Tools:        tools,
		Authorizer:   authorizer,
		Instructions: "Review the purchase using read-only tools. Suggest human next steps; never approve or pay.",
		Principal: func(d Dependencies) (agent.Actor, error) {
			if d.Actor.SubjectID == "" || d.Actor.Scope == "" {
				return agent.Actor{}, agent.ErrDenied
			}

			return d.Actor, nil
		},
		ValidateOutput: func(_ context.Context, _ Dependencies, review Review) error {
			if review.Summary == "" || len(review.NextSteps) == 0 {
				return errors.New("summary and next steps required")
			}

			return nil
		},
	})
}

func script() *scripted.Provider {
	final := scripted.Final(
		`{"summary":"Cedar Labs is approved; the $1,250 request exceeds the $1,000 review threshold.","disposition":"needs_review","next_steps":["Ask a human reviewer to check the purchase justification."]}`,
	)
	final.Check = func(req agent.Request) error {
		expected := []string{
			`{"id":"purchase-42","tenant_id":"cedar","vendor_id":"vendor-7","amount_cents":125000}`,
			`{"name":"Cedar Labs","standing":"approved"}`,
			`{"review_above_cents":100000}`,
		}

		if len(req.Messages) != 5 {
			return errors.New("missing tool history")
		}

		for i, want := range expected {
			if req.Messages[i+2].Role != "tool" ||
				req.Messages[i+2].Content != want {
				return errors.New("unexpected tool result")
			}
		}

		return nil
	}

	return scripted.New(scripted.Calls(
		agent.ToolCall{
			ID:        "purchase",
			Name:      "read_purchase",
			Arguments: `{"id":"purchase-42"}`,
		},
		agent.ToolCall{
			ID:        "vendor",
			Name:      "read_vendor",
			Arguments: `{"id":"vendor-7"}`,
		},
		agent.ToolCall{ID: "policy", Name: "read_policy", Arguments: `{}`},
	), final)
}

func demo(w io.Writer) error {
	var calls atomic.Int32

	server := fakeVendors(&calls)
	defer server.Close()

	deps := Dependencies{
		Actor: agent.Actor{SubjectID: "riley", Scope: "cedar"},
		Purchase: Purchase{
			ID:          "purchase-42",
			TenantID:    "cedar",
			VendorID:    "vendor-7",
			AmountCents: 125000,
		},
		Policy: Policy{ReviewAboveCents: 100000},
		Vendor: &VendorClient{http: server.Client(), baseURL: server.URL},
	}

	a, err := build(script())
	if err != nil {
		return err
	}

	result, err := a.Run(context.Background(), agent.Run[Dependencies]{
		ID: "review-001", Deps: deps, Input: "Review purchase-42.",
		Tools: agent.OnlyTools("read_purchase", "read_vendor", "read_policy"),
	})
	if err != nil {
		return err
	}

	if calls.Load() != 1 {
		return errors.New("expected one vendor call")
	}

	if err := json.NewEncoder(w).Encode(result.Output); err != nil {
		return err
	}

	for _, scenario := range []struct {
		name        string
		actor       string
		tool        string
		restriction agent.Restriction
	}{
		{name: "RBAC denial", actor: "sam", tool: "read_vendor"},
		{name: "privileged actor / narrow run", actor: "lee", tool: "read_vendor", restriction: agent.OnlyTools("read_purchase")},
		{name: "privileged actor / narrow agent", actor: "lee", tool: "pay_purchase"},
	} {
		calls.Store(0)

		deps.Actor.SubjectID = scenario.actor

		a, err := build(scripted.New(scripted.Calls(agent.ToolCall{
			ID: "denied", Name: scenario.tool, Arguments: `{"id":"vendor-7"}`,
		})))
		if err != nil {
			return err
		}

		_, err = a.Run(context.Background(), agent.Run[Dependencies]{
			ID: "denied-" + scenario.actor + scenario.tool, Deps: deps,
			Input: "Try the requested tool.", Tools: scenario.restriction,
		})
		if !errors.Is(err, agent.ErrDenied) || calls.Load() != 0 {
			return fmt.Errorf("%s did not fail closed: %w", scenario.name, err)
		}

		fmt.Fprintf(
			w,
			"%s: denied; external calls = %d\n",
			scenario.name,
			calls.Load(),
		)
	}

	return nil
}

func main() {
	if err := demo(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
