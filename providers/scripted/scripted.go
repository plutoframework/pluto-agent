// Package scripted provides a deterministic provider for runtime tests and
// credential-free examples. It is not a stochastic model-quality evaluator.
package scripted

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	agent "github.com/plutoframework/pluto-agent"
)

type Step struct {
	Response agent.Response
	Deltas   []string
	Err      error
	Check    func(agent.Request) error
}

// Provider consumes a shared sequence. Use one per run when scripts must not
// interleave. Steps and responses are copied at construction; Check must be
// concurrency-safe. No request payloads are retained by this provider.
type Provider struct {
	mu    sync.Mutex
	steps []Step
	next  int
}

func New(steps ...Step) *Provider {
	copied := make([]Step, len(steps))
	for i, step := range steps {
		copied[i] = step
		copied[i].Deltas = append([]string(nil), step.Deltas...)
		copied[i].Response.Output = append(
			json.RawMessage(nil),
			step.Response.Output...)
		copied[i].Response.Calls = append(
			[]agent.ToolCall(nil),
			step.Response.Calls...)
	}

	return &Provider{steps: copied}
}

func (p *Provider) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Tools:            true,
		StructuredOutput: true,
		Streaming:        true,
		Usage:            true,
	}
}

func (p *Provider) Generate(
	ctx context.Context,
	req agent.Request,
) (agent.Response, error) {
	return p.respond(ctx, req, nil)
}

func (p *Provider) Stream(
	ctx context.Context,
	req agent.Request,
	emit func(agent.Delta) error,
) (agent.Response, error) {
	return p.respond(ctx, req, emit)
}

func (p *Provider) respond(
	ctx context.Context,
	req agent.Request,
	emit func(agent.Delta) error,
) (agent.Response, error) {
	if err := ctx.Err(); err != nil {
		return agent.Response{}, err
	}

	p.mu.Lock()

	if p.next >= len(p.steps) {
		p.mu.Unlock()

		return agent.Response{}, fmt.Errorf("script exhausted")
	}

	step := p.steps[p.next]
	p.next++
	p.mu.Unlock()

	if step.Check != nil {
		if err := step.Check(req); err != nil {
			return agent.Response{}, err
		}
	}

	if emit != nil {
		for _, text := range step.Deltas {
			if err := ctx.Err(); err != nil {
				return agent.Response{}, err
			}

			if err := emit(agent.Delta{Text: text}); err != nil {
				return agent.Response{}, err
			}
		}
	}

	return step.Response, step.Err
}

func Final(output string) Step {
	return Step{Response: agent.Response{
		Finish: agent.FinishFinal, Output: json.RawMessage(output),
		Usage: agent.Usage{Known: true},
	}}
}

func Calls(calls ...agent.ToolCall) Step {
	return Step{Response: agent.Response{
		Finish: agent.FinishTools, Calls: calls,
		Usage: agent.Usage{Known: true},
	}}
}
