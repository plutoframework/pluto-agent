package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Actor is host-verified identity and scope. The library does not authenticate
// these strings. Extract them only from trusted application dependencies.
type Actor struct {
	SubjectID string
	Scope     string
}

type Authorizer interface {
	Authorize(context.Context, Actor, string) error
}

type AuthorizerFunc func(context.Context, Actor, string) error

func (f AuthorizerFunc) Authorize(
	ctx context.Context,
	a Actor,
	p string,
) error {
	if f == nil {
		return ErrDenied
	}

	return f(ctx, a, p)
}

// Access requires exactly one of Public=true or a nonempty Permission.
type Access struct {
	Public     bool
	Permission string
}

type Effect string

const (
	ReadOnly   Effect = "read_only"
	SideEffect Effect = "side_effect"
)

// ApprovalRequest binds approval to a run, tool, verified actor/scope, and the
// canonical validated arguments. Arguments are owned by the verifier. Hash is
// SHA-256 of Arguments, for storage convenience, not an authorization token.
type ApprovalRequest struct {
	RunID     string
	ToolID    string
	Actor     Actor
	Arguments json.RawMessage
	Hash      string
}

// ApprovalVerifier is a trusted host boundary, never a model-provided boolean.
// The host owns authentication, expiration, persistence, atomic consumption,
// and reuse policy. Returning nil authorizes only this exact request.
type ApprovalVerifier interface {
	Verify(context.Context, ApprovalRequest) error
}

type ToolConfig[D, A, R any] struct {
	Name             string
	Description      string
	Access           Access
	Effect           Effect
	ApprovalRequired bool
	Validate         func(context.Context, D, A) error
	// Policy is mandatory, including for public tools. It checks the actual
	// validated resource against the verified actor and host-owned scope.
	Policy         func(context.Context, D, Actor, A) error
	Handle         func(context.Context, D, A) (R, error)
	ValidateResult func(context.Context, D, R) error
}

// Tool is sealed; construct typed boundaries with NewTool.
type Tool[D any] interface {
	definition() ToolDefinition
	access() Access
	prepare(context.Context, D, Actor, []byte, int) (preparedTool, error)
}

type preparedTool struct {
	args     []byte
	effect   Effect
	approval bool
	policy   func(context.Context) error
	invoke   func(context.Context, int) ([]byte, error)
}

type typedTool[D, A, R any] struct {
	config ToolConfig[D, A, R]
	args   *Schema
	result *Schema
}

var toolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func NewTool[D, A, R any](c ToolConfig[D, A, R]) (Tool[D], error) {
	if !toolName.MatchString(c.Name) || len(c.Description) > 4096 ||
		c.Policy == nil || c.Handle == nil ||
		(c.Access.Public == (c.Access.Permission != "")) ||
		strings.TrimSpace(c.Access.Permission) != c.Access.Permission ||
		(c.Effect != ReadOnly && c.Effect != SideEffect) {
		return nil, fmt.Errorf("%w: invalid tool declaration", ErrConfig)
	}

	args, err := SchemaFor[A]()
	if err != nil {
		return nil, err
	}

	result, err := SchemaFor[R]()
	if err != nil {
		return nil, err
	}

	return &typedTool[D, A, R]{config: c, args: args, result: result}, nil
}

func (t *typedTool[D, A, R]) definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.config.Name,
		Description: t.config.Description,
		Schema:      t.args.JSON(),
	}
}

func (t *typedTool[D, A, R]) access() Access { return t.config.Access }

func (t *typedTool[D, A, R]) prepare(
	ctx context.Context,
	deps D,
	actor Actor,
	raw []byte,
	maxBytes int,
) (preparedTool, error) {
	var args A
	if err := t.args.Decode(raw, maxBytes, &args); err != nil {
		return preparedTool{}, err
	}

	if t.config.Validate != nil {
		if err := t.config.Validate(ctx, deps, args); err != nil {
			return preparedTool{}, fmt.Errorf(
				"%w: tool arguments",
				ErrValidation,
			)
		}
	}

	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxBytes {
		return preparedTool{}, ErrLimit
	}

	// Validate again: a validator can mutate nested slices. The canonical
	// value is the approved value; policies and handlers get separate copies.
	var checked A
	if err := t.args.Decode(canonical, maxBytes, &checked); err != nil {
		return preparedTool{}, err
	}

	return preparedTool{
		args:     canonical,
		effect:   t.config.Effect,
		approval: t.config.ApprovalRequired,
		policy: func(ctx context.Context) error {
			return t.config.Policy(ctx, deps, actor, checked)
		},
		invoke: func(ctx context.Context, maxOutput int) ([]byte, error) {
			var clean A
			if err := t.args.Decode(canonical, maxBytes, &clean); err != nil {
				return nil, err
			}

			result, err := t.config.Handle(ctx, deps, clean)
			if err != nil {
				return nil, err
			}

			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > maxOutput {
				return nil, ErrLimit
			}

			var validated R
			if err := t.result.Decode(
				encoded,
				maxOutput,
				&validated,
			); err != nil {
				return nil, err
			}

			if t.config.ValidateResult != nil {
				if err := t.config.ValidateResult(
					ctx,
					deps,
					validated,
				); err != nil {
					return nil, fmt.Errorf("%w: tool result", ErrValidation)
				}
			}

			return encoded, nil
		},
	}, nil
}

func argsHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Interface,
		reflect.Slice, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}
