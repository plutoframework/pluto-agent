package agent

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrConfig     = errors.New("invalid agent configuration")
	ErrCapability = errors.New("unsupported provider capability")
	ErrValidation = errors.New("schema or business validation failed")
	ErrDenied     = errors.New("tool access denied")
	ErrApproval   = errors.New("trusted approval required")
	ErrLimit      = errors.New("agent limit exceeded")
	ErrProvider   = errors.New("provider failed")
	ErrProtocol   = errors.New("invalid provider response")
	ErrReplay     = errors.New("replayed tool call")
)

// Capabilities describe the configured model, not an entire provider family.
type Capabilities struct {
	Tools            bool
	StructuredOutput bool
	Streaming        bool
	Usage            bool
}

type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is provider-neutral history. Tool results correlate by CallID.
type Message struct {
	Role    string
	Content string
	Calls   []ToolCall
	CallID  string
}

type Request struct {
	Instructions    string
	Messages        []Message
	Tools           []ToolDefinition
	OutputSchema    json.RawMessage
	MaxOutputTokens int64
}

type Finish string

const (
	FinishFinal   Finish = "final"
	FinishTools   Finish = "tools"
	FinishLength  Finish = "length"
	FinishRefusal Finish = "refusal"
)

// Usage is post-hoc provider accounting. Known=false never means zero tokens.
// On failure, totals include only reported completed calls and Known=false if
// any attempted call lacks accounting. Failed stream usage can be unavailable.
type Usage struct {
	Known        bool
	InputTokens  int64
	OutputTokens int64
}

type Response struct {
	Output json.RawMessage
	Calls  []ToolCall
	Finish Finish
	Usage  Usage
}

// Delta.Text is an unvalidated fragment, never a typed final result.
type Delta struct{ Text string }

// Provider implementations must honor context, bound transport bodies, and
// return emitter errors. Runtime requests are owned by each invocation.
type Provider interface {
	Capabilities() Capabilities
	Generate(context.Context, Request) (Response, error)
}

type StreamingProvider interface {
	Provider
	Stream(context.Context, Request, func(Delta) error) (Response, error)
}

// Operation is deliberately low-cardinality and contains no input or identity.
type Operation string

const (
	OperationRun   Operation = "run"
	OperationModel Operation = "model"
	OperationTool  Operation = "tool"
)

type Observation struct {
	Failed bool
	Usage  Usage
}

// Observer is an optional tracing seam. Callbacks must be concurrency-safe,
// non-panicking, and preserve cancellation and context values.
type Observer interface {
	Start(context.Context, Operation) (context.Context, func(Observation))
}
