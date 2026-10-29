package scripted_test

import (
	"context"
	"errors"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestScriptCopiesAndExhaustion(t *testing.T) {
	step := scripted.Final(`{"summary":"ok"}`)
	step.Deltas = []string{"first", "second"}
	p := scripted.New(step)
	step.Response.Output[0] = '!'
	step.Deltas[0] = "tampered"

	var got []string

	r, err := p.Stream(
		context.Background(),
		agent.Request{},
		func(d agent.Delta) error {
			got = append(got, d.Text)
			return nil
		},
	)
	if err != nil || string(r.Output) != `{"summary":"ok"}` ||
		got[0] != "first" {
		t.Fatalf("mutable script: %+v %v", r, err)
	}

	if _, err := p.Generate(context.Background(), agent.Request{}); err == nil {
		t.Fatal("script exhaustion hidden")
	}

	if !p.Capabilities().Streaming {
		t.Fatal("missing capability")
	}
}

func TestScriptChecksAndCancellation(t *testing.T) {
	want := errors.New("request mismatch")
	step := scripted.Final(`{}`)
	step.Check = func(agent.Request) error { return want }

	p := scripted.New(step)
	if _, err := p.Generate(
		context.Background(),
		agent.Request{},
	); !errors.Is(
		err,
		want,
	) {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Generate(
		ctx,
		agent.Request{},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}
}
