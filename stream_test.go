package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestStreamingPartialIsSeparateFromValidatedFinal(t *testing.T) {
	bad := scripted.Final(`{"summary":8}`)
	bad.Deltas = []string{`{"summary":`, `8}`}
	good := scripted.Final(`{"summary":"ok"}`)
	good.Deltas = []string{`{"summary":`, `"ok"}`}
	a, run := fixture(t, []scripted.Step{bad, good}, nil, nil)

	stream := a.Stream(context.Background(), run)
	defer stream.Close()

	partials, finals, repairs := 0, 0, 0

	for event := range stream.Events {
		switch event.Kind {
		case agent.EventPartial:
			partials++

			if event.Output != nil {
				t.Fatal("partial has trusted output")
			}
		case agent.EventFinal:
			finals++

			if event.Output == nil || event.Output.Summary != "ok" {
				t.Fatal("unvalidated final")
			}

			// Event delivery cannot mutate the returned result.
			event.Output.Summary = "tampered"
		case agent.EventRepair:
			repairs++
		}
	}

	r, err := stream.Wait()
	if err != nil || r.Output.Summary != "ok" ||
		r.Status != agent.StatusCompleted ||
		partials != 4 ||
		finals != 1 ||
		repairs != 1 {
		t.Fatalf(
			"stream: %+v %v partials=%d finals=%d repairs=%d",
			r,
			err,
			partials,
			finals,
			repairs,
		)
	}
}

func TestStreamBackpressureCanBeCanceledWithoutReading(t *testing.T) {
	a, run := fixture(
		t,
		[]scripted.Step{scripted.Final(`{"summary":"ok"}`)},
		nil,
		nil,
	)
	stream := a.Stream(context.Background(), run)
	stream.Close()

	r, err := stream.Wait()
	if !errors.Is(err, context.Canceled) || r.Status != agent.StatusCanceled {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestStreamConsumerFailureStopsProvider(t *testing.T) {
	step := scripted.Final(`{"summary":"ok"}`)
	step.Deltas = []string{"a", "b"}
	a, run := fixture(t, []scripted.Step{step}, nil, nil)
	want := errors.New("consumer disconnected")
	partials, finals := 0, 0

	_, err := a.RunStream(
		context.Background(),
		run,
		func(e agent.Event[output]) error {
			if e.Kind == agent.EventFinal {
				finals++
			}

			if e.Kind == agent.EventPartial {
				partials++
				return want
			}

			return nil
		},
	)
	if !errors.Is(err, want) || partials != 1 || finals != 0 {
		t.Fatalf("%v", err)
	}
}

func TestInvalidStreamNeverEmitsFinal(t *testing.T) {
	step := scripted.Final(`{"summary":false}`)
	step.Deltas = []string{`{"summary":false}`}
	a, run := fixture(
		t,
		[]scripted.Step{step},
		nil,
		func(c *agent.Config[deps, output]) {
			c.Limits = agent.DefaultLimits()
			c.Limits.MaxRepairs = 0
		},
	)
	finals := 0

	_, err := a.RunStream(
		context.Background(),
		run,
		func(e agent.Event[output]) error {
			if e.Kind == agent.EventFinal {
				finals++
			}

			return nil
		},
	)
	if !errors.Is(err, agent.ErrValidation) || finals != 0 {
		t.Fatalf("%v finals=%d", err, finals)
	}
}

func TestCancellationBeforeAndDuringHandler(t *testing.T) {
	started := make(chan struct{})
	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("1", "lookup", validArgs))},
		func(c *agent.ToolConfig[deps, args, output]) {
			c.Handle = func(ctx context.Context, d deps, _ args) (output, error) {
				d.calls.Add(1)
				close(started)
				<-ctx.Done()

				return output{}, ctx.Err()
			}
		},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := a.Run(ctx, run)
		done <- err
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("handler did not start")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || run.Deps.calls.Load() != 1 {
			t.Fatalf("%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not propagate")
	}

	a, run = fixture(t, nil, nil, nil)

	_, err := a.Run(ctx, run)
	if !errors.Is(err, context.Canceled) || run.Deps.calls.Load() != 0 {
		t.Fatalf("pre-canceled: %v", err)
	}
}

func TestToolDeadlineIsCooperativeAndNotRetried(t *testing.T) {
	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("1", "lookup", validArgs))},
		func(c *agent.ToolConfig[deps, args, output]) {
			c.Handle = func(ctx context.Context, d deps, _ args) (output, error) {
				d.calls.Add(1)
				<-ctx.Done()

				return output{}, ctx.Err()
			}
		},
		func(c *agent.Config[deps, output]) {
			c.Limits = agent.DefaultLimits()
			c.Limits.ToolTimeout = time.Millisecond
		},
	)

	r, err := a.Run(context.Background(), run)
	if !errors.Is(err, context.DeadlineExceeded) ||
		r.Status != agent.StatusCanceled ||
		run.Deps.calls.Load() != 1 ||
		r.Repairs != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestCanceledPermissionCallbackCannotDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, run := fixture(
		t,
		[]scripted.Step{scripted.Calls(call("1", "lookup", validArgs))},
		nil,
		func(c *agent.Config[deps, output]) {
			c.Authorizer = agent.AuthorizerFunc(
				func(context.Context, agent.Actor, string) error {
					cancel()

					return nil
				},
			)
		},
	)

	_, err := a.Run(ctx, run)
	if !errors.Is(err, context.Canceled) || run.Deps.calls.Load() != 0 {
		t.Fatalf("%v", err)
	}
}
