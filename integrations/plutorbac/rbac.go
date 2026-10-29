// Package plutorbac adapts the published Pluto RBAC authorizer. It contains no
// grant algorithm or assignment cache. Configure freshness in the RBAC resolver.
package plutorbac

import (
	"context"
	"reflect"

	agent "github.com/plutoframework/pluto-agent"

	rbac "github.com/plutoframework/pluto-rbac"
)

type Checker interface {
	Authorize(context.Context, rbac.Request) error
}

type Authorizer struct{ checker Checker }

func New(checker Checker) (*Authorizer, error) {
	if checker == nil {
		return nil, agent.ErrConfig
	}

	v := reflect.ValueOf(checker)
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Func) && v.IsNil() {
		return nil, agent.ErrConfig
	}

	return &Authorizer{checker: checker}, nil
}

func (a *Authorizer) Authorize(
	ctx context.Context,
	actor agent.Actor,
	permission string,
) error {
	if a == nil || a.checker == nil {
		return agent.ErrDenied
	}

	return a.checker.Authorize(ctx, rbac.Request{
		SubjectID:  actor.SubjectID,
		Scope:      rbac.Scope{TenantID: actor.Scope},
		Permission: rbac.PermissionID(permission),
	})
}
