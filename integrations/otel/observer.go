// Package otel implements agent.Observer with the OpenTelemetry trace API.
// Pass pluto-core/telemetry.Runtime.Tracer or any compatible tracer. This
// package configures no global provider, exporter, SDK, or durable audit store.
package otel

import (
	"context"
	"sync"

	agent "github.com/plutoframework/pluto-agent"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Observer struct{ tracer trace.Tracer }

func New(tracer trace.Tracer) (*Observer, error) {
	if tracer == nil {
		return nil, agent.ErrConfig
	}

	return &Observer{tracer: tracer}, nil
}

func (o *Observer) Start(
	ctx context.Context,
	op agent.Operation,
) (context.Context, func(agent.Observation)) {
	name := "agent.other"

	switch op {
	case agent.OperationRun:
		name = "agent.run"
	case agent.OperationModel:
		name = "agent.model"
	case agent.OperationTool:
		name = "agent.tool"
	}

	ctx, span := o.tracer.Start(ctx, name)

	var once sync.Once

	return ctx, func(observation agent.Observation) {
		once.Do(func() {
			if observation.Failed {
				span.SetStatus(codes.Error, "agent operation failed")
			}

			span.SetAttributes(
				attribute.Bool("agent.failed", observation.Failed),
			)

			if observation.Usage.Known &&
				(op == agent.OperationModel || op == agent.OperationRun) {
				span.SetAttributes(
					attribute.Int64(
						"gen_ai.usage.input_tokens",
						bounded(observation.Usage.InputTokens),
					),
					attribute.Int64(
						"gen_ai.usage.output_tokens",
						bounded(observation.Usage.OutputTokens),
					),
				)
			}

			span.End()
		})
	}
}

func bounded(value int64) int64 { return max(0, min(value, 1_000_000_000)) }
