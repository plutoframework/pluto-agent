package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

func (a *Agent[D, O]) Run(ctx context.Context, run Run[D]) (Result[O], error) {
	return a.execute(ctx, run, nil)
}

type runState[D, O any] struct {
	agent     *Agent[D, O]
	run       Run[D]
	actor     Actor
	allowed   map[string]bool
	result    Result[O]
	messages  []Message
	callIDs   map[string]bool
	writes    map[string]bool
	emit      func(Event[O]) error
	streaming bool
}

func (a *Agent[D, O]) execute(
	ctx context.Context,
	run Run[D],
	emit func(Event[O]) error,
) (result Result[O], err error) {
	result.Status = StatusFailed
	if ctx == nil || a == nil || a.output == nil || !validRunID(run.ID) {
		return result, ErrConfig
	}

	ctx, cancel := context.WithTimeout(ctx, a.config.Limits.RunTimeout)
	defer cancel()

	ctx, end := a.observe(ctx, OperationRun)

	defer func() {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			result.Status = StatusCanceled
		} else if errors.Is(err, ErrLimit) {
			result.Status = StatusLimited
		}

		end(Observation{Failed: err != nil, Usage: result.Usage})
	}()

	if err := ctx.Err(); err != nil {
		return result, err
	}

	if len(run.Input) > a.config.Limits.MaxInputBytes ||
		!utf8.ValidString(run.Input) {
		return result, ErrLimit
	}

	allowed, err := a.allowed(run.Tools)
	if err != nil {
		return result, err
	}

	actor, err := a.config.Principal(run.Deps)
	if err != nil {
		return result, fmt.Errorf("%w: principal", ErrDenied)
	}

	s := runState[D, O]{
		agent: a, run: run, actor: actor, allowed: allowed,
		result:   result,
		messages: []Message{{Role: "user", Content: run.Input}},
		callIDs:  make(map[string]bool), writes: make(map[string]bool),
		emit:      emit,
		streaming: emit != nil,
	}
	if s.emit == nil {
		s.emit = func(Event[O]) error { return nil }
	}

	err = s.loop(ctx)

	return s.result, err
}

func (s *runState[D, O]) loop(ctx context.Context) error {
	limits := s.agent.config.Limits

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if s.result.ModelCalls >= limits.MaxModelCalls {
			return ErrLimit
		}

		response, err := s.model(ctx)
		if err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if err := s.account(response.Usage); err != nil {
			return err
		}

		size := len(response.Output)
		if len(response.Calls) > limits.MaxToolCalls {
			return ErrLimit
		}

		for _, call := range response.Calls {
			if len(call.Arguments) > limits.MaxModelOutputBytes ||
				len(call.ID) > 128 || len(call.Name) > 64 {
				return ErrLimit
			}

			size += len(call.Arguments) + len(call.ID) + len(call.Name)
		}

		if size > limits.MaxModelOutputBytes {
			return ErrLimit
		}

		switch response.Finish {
		case FinishTools:
			if len(response.Output) != 0 || len(response.Calls) == 0 {
				return ErrProtocol
			}

			if err := s.tools(ctx, response.Calls); err != nil {
				return err
			}
		case FinishFinal:
			if len(response.Calls) != 0 {
				return ErrProtocol
			}

			var output O

			err := s.agent.output.Decode(
				response.Output, limits.MaxModelOutputBytes, &output,
			)
			if err == nil && s.agent.config.ValidateOutput != nil {
				err = s.agent.config.ValidateOutput(ctx, s.run.Deps, output)
			}

			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}

			if err != nil {
				if err := s.repair(); err != nil {
					return err
				}

				s.messages = append(
					s.messages,
					Message{
						Role:    "assistant",
						Content: string(response.Output),
					},
					Message{
						Role:    "user",
						Content: "Return valid JSON matching the output schema and application constraints.",
					},
				)

				continue
			}

			// Validators and event consumers must not share mutable output.
			canonical, err := json.Marshal(output)
			if err != nil {
				return ErrValidation
			}

			var eventOutput O
			if err := s.agent.output.Decode(
				canonical,
				limits.MaxModelOutputBytes,
				&eventOutput,
			); err != nil {
				return err
			}

			if err := s.emit(
				Event[O]{Kind: EventFinal, Output: &eventOutput},
			); err != nil {
				return err
			}

			if err := ctx.Err(); err != nil {
				return err
			}

			s.result.Output = output
			s.result.Status = StatusCompleted

			return nil
		case FinishLength:
			return fmt.Errorf("%w: provider truncated output", ErrLimit)
		case FinishRefusal:
			return fmt.Errorf("%w: model refused", ErrProvider)
		default:
			return ErrProtocol
		}
	}
}

func (s *runState[D, O]) model(ctx context.Context) (Response, error) {
	l := s.agent.config.Limits

	remaining := l.MaxTokens - s.result.Usage.InputTokens - s.result.Usage.OutputTokens
	if remaining <= 0 {
		return Response{}, ErrLimit
	}

	request := Request{
		Instructions:    s.agent.config.Instructions,
		Messages:        s.messages,
		OutputSchema:    s.agent.output.JSON(),
		MaxOutputTokens: min(l.MaxOutputTokens, remaining),
	}
	for _, name := range s.agent.order {
		if s.allowed[name] {
			request.Tools = append(
				request.Tools,
				s.agent.tools[name].definition(),
			)
		}
	}

	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > l.MaxRequestBytes {
		return Response{}, ErrLimit
	}

	// Isolate history from a provider that mutates its request or keeps it.
	request = Request{}
	if err := json.Unmarshal(encoded, &request); err != nil {
		return Response{}, ErrProtocol
	}

	ctx, cancel := context.WithTimeout(ctx, l.ModelTimeout)
	defer cancel()

	ctx, end := s.agent.observe(ctx, OperationModel)

	s.result.ModelCalls++
	if err := s.emit(Event[O]{Kind: EventModelStart}); err != nil {
		end(Observation{Failed: true})

		return Response{}, err
	}

	var response Response

	if s.runStreaming() {
		var bytes int

		response, err = s.agent.config.Provider.(StreamingProvider).Stream(
			ctx, request, func(delta Delta) error {
				if len(delta.Text) > l.MaxModelOutputBytes-bytes {
					return ErrLimit
				}

				bytes += len(delta.Text)

				if err := ctx.Err(); err != nil {
					return err
				}

				return s.emit(Event[O]{Kind: EventPartial, Partial: delta.Text})
			},
		)
	} else {
		response, err = s.agent.config.Provider.Generate(ctx, request)
	}

	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}

	end(Observation{Failed: err != nil, Usage: response.Usage})

	if err != nil {
		s.result.Usage.Known = false
		return Response{}, fmt.Errorf("%w: %w", ErrProvider, err)
	}

	return response, nil
}

func (s *runState[D, O]) runStreaming() bool {
	return s.emit != nil && s.streaming
}

func (s *runState[D, O]) account(u Usage) error {
	if !u.Known || u.InputTokens < 0 || u.OutputTokens < 0 ||
		u.InputTokens > math.MaxInt64-u.OutputTokens {
		s.result.Usage.Known = false
		return fmt.Errorf("%w: missing or invalid usage", ErrProtocol)
	}

	used := s.result.Usage.InputTokens + s.result.Usage.OutputTokens
	if u.InputTokens+u.OutputTokens > math.MaxInt64-used {
		s.result.Usage.Known = false
		return ErrLimit
	}

	s.result.Usage.Known = true
	s.result.Usage.InputTokens += u.InputTokens

	s.result.Usage.OutputTokens += u.OutputTokens
	if used+u.InputTokens+u.OutputTokens > s.agent.config.Limits.MaxTokens {
		return ErrLimit
	}

	return nil
}

func (s *runState[D, O]) repair() error {
	if s.result.Repairs >= s.agent.config.Limits.MaxRepairs {
		return fmt.Errorf("%w: repair attempts exhausted", ErrValidation)
	}

	s.result.Repairs++

	return s.emit(Event[O]{Kind: EventRepair})
}
