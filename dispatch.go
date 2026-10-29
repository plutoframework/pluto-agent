package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

func (s *runState[D, O]) tools(ctx context.Context, calls []ToolCall) error {
	if len(calls) > s.agent.config.Limits.MaxToolCalls-s.result.ToolCalls {
		return ErrLimit
	}

	// Check the entire batch's routing and IDs before dispatching anything.
	for _, call := range calls {
		if call.ID == "" || !s.allowed[call.Name] ||
			s.agent.tools[call.Name] == nil {
			return ErrDenied
		}

		if s.callIDs[call.ID] {
			return ErrReplay
		}

		s.callIDs[call.ID] = true
	}

	s.messages = append(s.messages, Message{Role: "assistant", Calls: calls})
	for _, call := range calls {
		s.result.ToolCalls++

		output, repairable, err := s.dispatch(ctx, call)
		if err != nil {
			if !repairable {
				return err
			}

			if err := s.repair(); err != nil {
				return err
			}

			output = []byte(
				`{"error":"invalid tool arguments; use the declared schema and constraints"}`,
			)
		}

		s.messages = append(s.messages, Message{
			Role: "tool", Content: string(output), CallID: call.ID,
		})
	}

	return nil
}

func (s *runState[D, O]) dispatch(
	ctx context.Context,
	call ToolCall,
) (output []byte, repairable bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.agent.config.Limits.ToolTimeout)
	defer cancel()

	ctx, end := s.agent.observe(ctx, OperationTool)
	defer func() {
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
			repairable = false
		}

		end(Observation{Failed: err != nil})
	}()

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := s.emit(
		Event[O]{Kind: EventToolStart, Tool: call.Name},
	); err != nil {
		return nil, false, err
	}

	tool := s.agent.tools[call.Name]

	access := tool.access()
	if !access.Public {
		if s.actor.SubjectID == "" {
			return nil, false, ErrDenied
		}

		if err := s.agent.config.Authorizer.Authorize(
			ctx, s.actor, access.Permission,
		); err != nil {
			return nil, false, fmt.Errorf("%w: %w", ErrDenied, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	prepared, err := tool.prepare(
		ctx, s.run.Deps, s.actor, []byte(call.Arguments),
		s.agent.config.Limits.MaxModelOutputBytes,
	)
	if err != nil {
		return nil, errors.Is(err, ErrValidation), err
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := prepared.policy(ctx); err != nil {
		return nil, false, fmt.Errorf("%w: resource policy", ErrDenied)
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if prepared.approval {
		if s.actor.SubjectID == "" || nilValue(s.agent.config.Approvals) {
			return nil, false, ErrApproval
		}

		if err := s.agent.config.Approvals.Verify(ctx, ApprovalRequest{
			RunID:  s.run.ID,
			ToolID: call.Name,
			Actor:  s.actor,
			Arguments: bytes.Clone(
				prepared.args,
			),
			Hash: argsHash(prepared.args),
		}); err != nil {
			return nil, false, ErrApproval
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if prepared.effect == SideEffect {
		key := call.Name + ":" + argsHash(prepared.args)
		if s.writes[key] {
			return nil, false, ErrReplay
		}

		s.writes[key] = true
	}

	output, err = prepared.invoke(ctx, s.agent.config.Limits.MaxToolOutputBytes)
	if err != nil {
		return nil, false, err
	}

	if err := s.emit(
		Event[O]{Kind: EventToolEnd, Tool: call.Name},
	); err != nil {
		return nil, false, err
	}

	return output, false, nil
}
