package openai

import (
	"encoding/json"
	"fmt"

	agent "github.com/plutoframework/pluto-agent"
)

type function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireCall struct {
	Index    int      `json:"index"`
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function function `json:"function"`
}

type wireMessage struct {
	Role    string     `json:"role"`
	Content *string    `json:"content"`
	Refusal *string    `json:"refusal"`
	Calls   []wireCall `json:"tool_calls"`
}

type wireUsage struct {
	Input  *int64 `json:"prompt_tokens"`
	Output *int64 `json:"completion_tokens"`
}

type choice struct {
	Index   int         `json:"index"`
	Message wireMessage `json:"message"`
	Delta   wireMessage `json:"delta"`
	Finish  string      `json:"finish_reason"`
}

type completion struct {
	Choices []choice        `json:"choices"`
	Usage   *wireUsage      `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

func (p *Provider) request(req agent.Request, stream bool) ([]byte, error) {
	if req.MaxOutputTokens <= 0 || !json.Valid(req.OutputSchema) {
		return nil, agent.ErrConfig
	}

	messages := []map[string]any{
		{"role": "system", "content": req.Instructions},
	}
	for _, message := range req.Messages {
		m := map[string]any{"role": message.Role, "content": message.Content}
		switch message.Role {
		case "user":
			if len(message.Calls) != 0 || message.CallID != "" {
				return nil, agent.ErrProtocol
			}
		case "assistant":
			if len(message.Calls) > 0 {
				calls := make([]map[string]any, 0, len(message.Calls))
				for _, call := range message.Calls {
					calls = append(calls, map[string]any{
						"id":   call.ID,
						"type": "function",
						"function": function{
							Name:      call.Name,
							Arguments: call.Arguments,
						},
					})
				}

				m["tool_calls"] = calls
				m["content"] = nil
			}
		case "tool":
			if message.CallID == "" {
				return nil, agent.ErrProtocol
			}

			m["tool_call_id"] = message.CallID
		default:
			return nil, agent.ErrProtocol
		}

		messages = append(messages, m)
	}

	body := map[string]any{
		"reasoning_effort":      "none",
		"model":                 p.model,
		"messages":              messages,
		"store":                 false,
		"stream":                stream,
		"max_completion_tokens": req.MaxOutputTokens,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "agent_output",
				"strict": true,
				"schema": req.OutputSchema,
			},
		},
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if !json.Valid(tool.Schema) {
				return nil, agent.ErrConfig
			}

			tools = append(tools, map[string]any{
				"type": "function", "function": map[string]any{
					"name": tool.Name, "description": tool.Description,
					"parameters": tool.Schema, "strict": true,
				},
			})
		}

		body["tools"] = tools
		body["parallel_tool_calls"] = false
	}

	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	return json.Marshal(body)
}

func (p *Provider) finish(
	message wireMessage,
	finish string,
	usage *wireUsage,
) (agent.Response, error) {
	if usage == nil || usage.Input == nil || usage.Output == nil ||
		*usage.Input < 0 || *usage.Output < 0 || message.Role != "assistant" {
		return agent.Response{}, agent.ErrProtocol
	}

	r := agent.Response{Usage: agent.Usage{
		Known: true, InputTokens: *usage.Input, OutputTokens: *usage.Output,
	}}
	if message.Refusal != nil && *message.Refusal != "" {
		r.Finish = agent.FinishRefusal
		return r, nil
	}

	switch finish {
	case "stop":
		if message.Content == nil || len(message.Calls) != 0 {
			return agent.Response{}, agent.ErrProtocol
		}

		if len(*message.Content) > p.maxOutput {
			return agent.Response{}, agent.ErrLimit
		}

		r.Finish = agent.FinishFinal
		r.Output = json.RawMessage(*message.Content)
	case "tool_calls":
		if len(message.Calls) == 0 || len(message.Calls) > 128 {
			return agent.Response{}, agent.ErrProtocol
		}

		if message.Content != nil && *message.Content != "" {
			return agent.Response{}, fmt.Errorf(
				"%w: mixed text and calls",
				agent.ErrCapability,
			)
		}

		r.Finish = agent.FinishTools
		size := 0

		ids := make(map[string]bool)
		for _, call := range message.Calls {
			if call.Type != "function" || call.ID == "" ||
				call.Function.Name == "" || len(call.ID) > 128 ||
				len(call.Function.Name) > 64 || ids[call.ID] {
				return agent.Response{}, agent.ErrProtocol
			}

			ids[call.ID] = true

			size += len(
				call.Function.Arguments,
			) + len(
				call.ID,
			) + len(
				call.Function.Name,
			)
			if size > p.maxOutput {
				return agent.Response{}, agent.ErrLimit
			}

			r.Calls = append(r.Calls, agent.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
	case "length":
		r.Finish = agent.FinishLength
	case "content_filter":
		r.Finish = agent.FinishRefusal
	default:
		return agent.Response{}, agent.ErrProtocol
	}

	return r, nil
}
