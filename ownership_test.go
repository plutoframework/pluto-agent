package agent_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestProviderCannotMutateRetainedHistory(t *testing.T) {
	first := scripted.Calls(call("one", "lookup", validArgs))
	first.Check = func(req agent.Request) error {
		req.Messages[0].Content = "replaced input"
		req.Tools[0].Schema[0] = '!'

		return nil
	}
	second := scripted.Final(`{"summary":"ok"}`)
	second.Check = func(req agent.Request) error {
		if req.Messages[0].Content != "review" ||
			req.Tools[0].Schema[0] != '{' {
			return errors.New("provider mutated retained state")
		}

		return nil
	}

	a, run := fixture(t, []scripted.Step{first, second}, nil, nil)
	if _, err := a.Run(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRunsKeepIndependentState(t *testing.T) {
	const count = 24

	steps := make([]scripted.Step, count)
	for i := range steps {
		steps[i] = scripted.Final(`{"summary":"ok"}`)
	}

	a, run := fixture(t, steps, nil, nil)

	var group sync.WaitGroup
	for i := range count {
		group.Add(1)

		go func() {
			defer group.Done()

			ownRun := run
			ownRun.ID = fmt.Sprintf("run-%d", i)

			r, err := a.Run(context.Background(), ownRun)
			if err != nil || r.ModelCalls != 1 || r.Output.Summary != "ok" {
				t.Errorf("%+v %v", r, err)
			}
		}()
	}

	group.Wait()
}

type blockingProvider struct{}

func (blockingProvider) Capabilities() agent.Capabilities {
	return agent.Capabilities{Tools: true, StructuredOutput: true, Usage: true}
}

func (blockingProvider) Generate(
	ctx context.Context,
	_ agent.Request,
) (agent.Response, error) {
	<-ctx.Done()

	return agent.Response{}, ctx.Err()
}

func TestModelDeadlineAndUnknownFailedUsage(t *testing.T) {
	a, run := fixture(t, nil, nil, func(c *agent.Config[deps, output]) {
		c.Provider = blockingProvider{}
		c.Limits = agent.DefaultLimits()
		c.Limits.ModelTimeout = time.Millisecond
	})

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, context.DeadlineExceeded) ||
		r.Status != agent.StatusCanceled ||
		r.Usage.Known {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestFailedCallMakesAggregateUsageIncomplete(t *testing.T) {
	step := scripted.Calls(call("one", "lookup", validArgs))
	step.Response.Usage = agent.Usage{
		Known:        true,
		InputTokens:  4,
		OutputTokens: 2,
	}
	a, run := fixture(
		t,
		[]scripted.Step{step, {Err: errors.New("lost response")}},
		nil,
		nil,
	)

	r, err := a.Run(context.Background(), run)
	if err == nil || r.Usage.Known || r.Usage.InputTokens != 4 ||
		r.Usage.OutputTokens != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}
