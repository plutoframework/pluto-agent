package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type Config[D, O any] struct {
	Provider       Provider
	Instructions   string
	Tools          []Tool[D]
	Principal      func(D) (Actor, error)
	Authorizer     Authorizer
	Approvals      ApprovalVerifier
	ValidateOutput func(context.Context, D, O) error
	Observer       Observer
	// Zero Limits selects DefaultLimits. Other values must be complete.
	Limits Limits
}

// Agent is immutable after construction. Dependencies and all injected
// callbacks must support concurrency if the agent is used concurrently.
type Agent[D, O any] struct {
	config Config[D, O]
	tools  map[string]Tool[D]
	order  []string
	output *Schema
}

func New[D, O any](c Config[D, O]) (*Agent[D, O], error) {
	if c.Limits == (Limits{}) {
		c.Limits = DefaultLimits()
	}

	if nilValue(c.Provider) || c.Principal == nil || !c.Limits.valid() ||
		!utf8.ValidString(c.Instructions) ||
		len(c.Instructions) > c.Limits.MaxInputBytes {
		return nil, ErrConfig
	}

	capabilities := c.Provider.Capabilities()
	if !capabilities.StructuredOutput || !capabilities.Usage ||
		(len(c.Tools) > 0 && !capabilities.Tools) {
		return nil, ErrCapability
	}

	output, err := SchemaFor[O]()
	if err != nil {
		return nil, err
	}

	a := &Agent[D, O]{
		config: c,
		tools:  make(map[string]Tool[D]),
		output: output,
	}
	for _, tool := range c.Tools {
		if nilValue(tool) {
			return nil, ErrConfig
		}

		name := tool.definition().Name
		if _, exists := a.tools[name]; exists {
			return nil, fmt.Errorf("%w: duplicate tool", ErrConfig)
		}

		if !tool.access().Public && nilValue(c.Authorizer) {
			return nil, fmt.Errorf("%w: missing authorizer", ErrConfig)
		}

		a.tools[name] = tool
		a.order = append(a.order, name)
	}

	a.config.Tools = nil

	return a, nil
}

type Run[D any] struct {
	// ID is a host-generated unique run identifier, never model-generated.
	ID    string
	Deps  D
	Input string
	Tools Restriction
}

type Status string

const (
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusLimited   Status = "limited"
)

type Result[O any] struct {
	Output     O
	Status     Status
	Usage      Usage
	ModelCalls int
	ToolCalls  int
	Repairs    int
}

func (a *Agent[D, O]) allowed(r Restriction) (map[string]bool, error) {
	allowed := make(map[string]bool)

	if !r.set {
		for name := range a.tools {
			allowed[name] = true
		}

		return allowed, nil
	}

	for _, name := range r.ids {
		if _, exists := a.tools[name]; !exists {
			return nil, fmt.Errorf("%w: unknown run tool", ErrDenied)
		}

		allowed[name] = true
	}

	return allowed, nil
}

func validRunID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) &&
		strings.TrimSpace(id) == id
}

func (a *Agent[D, O]) observe(
	ctx context.Context,
	operation Operation,
) (context.Context, func(Observation)) {
	if nilValue(a.config.Observer) {
		return ctx, func(Observation) {}
	}

	return a.config.Observer.Start(ctx, operation)
}
