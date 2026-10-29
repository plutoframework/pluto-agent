# Pluto RBAC adapter

Module: `github.com/plutoframework/pluto-agent/integrations/plutorbac v0.1.2`
with published `github.com/plutoframework/pluto-rbac v0.1.0`.

Construct the application's immutable RBAC policy and assignment resolver using
`rbac.New` and `rbac.NewAuthorizer`. Then call `plutorbac.New(authorizer)` and
inject the result into `agent.Config.Authorizer`. The adapter maps
`agent.Actor.SubjectID`, `Actor.Scope`, and the declared permission exactly to
an RBAC request; scope becomes `rbac.Scope.TenantID`.

No grant algorithm, permission cache, tenant fallback, or implicit administrator
is introduced. Resolver freshness/concurrency are application responsibilities.
Keep resource ownership policy in the tool's mandatory `Policy`. See the
purchase-review example for a complete composition using published versions.
