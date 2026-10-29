package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	agent "github.com/plutoframework/pluto-agent"
)

type streamState struct {
	message     wireMessage
	finish      string
	usage       *wireUsage
	outputBytes int
	chunks      int
}

func (p *Provider) decodeStream(
	ctx context.Context,
	reader io.Reader,
	emit func(agent.Delta) error,
) (agent.Response, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, p.maxBody+1))
	scanner.Buffer(make([]byte, 4096), int(min(p.maxBody+1, 128<<10)))

	state := streamState{}

	var (
		event     strings.Builder
		bytesRead int64
	)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return agent.Response{}, err
		}

		line := scanner.Text()

		bytesRead += int64(len(line)) + 1
		if bytesRead > p.maxBody {
			return agent.Response{}, agent.ErrLimit
		}

		if line == "" {
			if event.Len() == 0 {
				continue
			}

			data := strings.TrimSuffix(event.String(), "\n")
			event.Reset()

			if data == "[DONE]" {
				if state.finish == "" || state.usage == nil {
					return agent.Response{}, agent.ErrProtocol
				}

				return p.finish(state.message, state.finish, state.usage)
			}

			if err := p.chunk(&state, data, emit); err != nil {
				return agent.Response{}, err
			}

			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			return agent.Response{}, agent.ErrProtocol
		}

		event.WriteString(
			strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "),
		)
		event.WriteByte('\n')
	}

	if scanner.Err() != nil {
		return agent.Response{}, agent.ErrLimit
	}

	// No final result from a disconnected/truncated stream, even if a full
	// JSON fragment happened to arrive before the transport ended.
	return agent.Response{}, agent.ErrProtocol
}

func (p *Provider) chunk(
	state *streamState,
	data string,
	emit func(agent.Delta) error,
) error {
	state.chunks++
	if state.chunks > 8192 {
		return agent.ErrLimit
	}

	var wire completion
	if err := json.Unmarshal(
		[]byte(data),
		&wire,
	); err != nil ||
		len(wire.Error) != 0 {
		return agent.ErrProtocol
	}

	if wire.Usage != nil {
		if state.usage != nil {
			return agent.ErrProtocol
		}

		state.usage = wire.Usage
	}

	if len(wire.Choices) == 0 {
		if wire.Usage == nil || state.finish == "" {
			return agent.ErrProtocol
		}

		return nil
	}

	if len(wire.Choices) != 1 || wire.Choices[0].Index != 0 ||
		state.finish != "" {
		return agent.ErrProtocol
	}

	choice := wire.Choices[0]

	delta := choice.Delta
	if delta.Role != "" {
		if delta.Role != "assistant" || state.message.Role != "" {
			return agent.ErrProtocol
		}

		state.message.Role = delta.Role
	}

	if delta.Refusal != nil {
		if state.message.Refusal == nil {
			state.message.Refusal = new(string)
		}

		state.outputBytes += len(*delta.Refusal)
		if state.outputBytes > p.maxOutput {
			return agent.ErrLimit
		}

		*state.message.Refusal += *delta.Refusal
	}

	if delta.Content != nil {
		state.outputBytes += len(*delta.Content)
		if state.outputBytes > p.maxOutput {
			return agent.ErrLimit
		}

		if state.message.Content == nil {
			state.message.Content = new(string)
		}

		*state.message.Content += *delta.Content
		if *delta.Content != "" {
			if err := emit(agent.Delta{Text: *delta.Content}); err != nil {
				return err
			}
		}
	}

	for _, call := range delta.Calls {
		if call.Index < 0 || call.Index > len(state.message.Calls) ||
			call.Index >= 128 {
			return agent.ErrProtocol
		}

		if call.Index == len(state.message.Calls) {
			state.message.Calls = append(state.message.Calls, wireCall{})
		}

		acc := &state.message.Calls[call.Index]
		if call.Type != "" {
			if call.Type != "function" || acc.Type != "" {
				return agent.ErrProtocol
			}

			acc.Type = call.Type
		}

		state.outputBytes += len(
			call.ID,
		) + len(
			call.Function.Name,
		) + len(
			call.Function.Arguments,
		)
		if state.outputBytes > p.maxOutput {
			return agent.ErrLimit
		}

		acc.ID += call.ID
		acc.Function.Name += call.Function.Name
		acc.Function.Arguments += call.Function.Arguments
	}

	state.finish = choice.Finish

	return nil
}
