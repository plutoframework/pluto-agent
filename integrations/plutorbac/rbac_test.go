package plutorbac_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/integrations/plutorbac"

	rbac "github.com/plutoframework/pluto-rbac"
)

func TestPublishedAuthorizerChecksFreshAssignmentsAndExactScope(t *testing.T) {
	policy, err := rbac.New(rbac.Definition{
		Permissions: []rbac.Permission{
			{ID: "read", Name: "Read", Description: "Read records."},
		},
		Roles: []rbac.Role{
			{
				ID:          "reader",
				Name:        "Reader",
				Description: "Reads records.",
				Permissions: []rbac.PermissionID{"read"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		revoked atomic.Bool
		checks  atomic.Int32
	)

	resolver := rbac.RoleResolverFunc(
		func(_ context.Context, subject string, scope rbac.Scope) ([]rbac.RoleID, error) {
			checks.Add(1)

			if subject != "server-verified" || scope.TenantID != "north" ||
				revoked.Load() {
				return nil, nil
			}

			return []rbac.RoleID{"reader"}, nil
		},
	)

	checker, err := rbac.NewAuthorizer(
		policy,
		resolver,
		rbac.Options{RequireTenant: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	a, err := plutorbac.New(checker)
	if err != nil {
		t.Fatal(err)
	}

	actor := agent.Actor{SubjectID: "server-verified", Scope: "north"}
	if err := a.Authorize(context.Background(), actor, "read"); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []agent.Actor{{SubjectID: "other", Scope: "north"}, {SubjectID: "server-verified", Scope: "south"}} {
		if err := a.Authorize(
			context.Background(),
			invalid,
			"read",
		); !errors.Is(
			err,
			rbac.ErrForbidden,
		) {
			t.Fatalf("scope widened: %v", err)
		}
	}

	revoked.Store(true)

	if err := a.Authorize(
		context.Background(),
		actor,
		"read",
	); !errors.Is(
		err,
		rbac.ErrForbidden,
	) {
		t.Fatalf("revocation cached: %v", err)
	}

	if checks.Load() != 4 {
		t.Fatalf("expected four current resolutions, got %d", checks.Load())
	}

	if err := a.Authorize(
		context.Background(),
		actor,
		"unknown",
	); !errors.Is(
		err,
		rbac.ErrForbidden,
	) {
		t.Fatalf("unknown permission: %v", err)
	}
}

func TestNilCheckerFails(t *testing.T) {
	var checker *rbac.Authorizer
	if _, err := plutorbac.New(checker); !errors.Is(err, agent.ErrConfig) {
		t.Fatal(err)
	}
}
