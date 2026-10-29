# Synthetic purchase review

Run `go run github.com/plutoframework/pluto-agent/examples/purchase-review@v0.1.2`
or `./scripts/example.sh` from the repository root. All module requirements are
published pins; no local replacements are committed.

The application injects a verified actor/scope, purchase, organization policy,
and HTTP client. Explicit `read_purchase`, `read_vendor`, and `read_policy` tools
use the published Pluto RBAC authorizer plus resource-specific policies.
Model arguments contain resource IDs only. A local synthetic vendor service
listens on an automatically allocated loopback port. No keys or money are used.

The successful run returns a typed review and suggested human next steps.
Additional scenarios prove RBAC denial produces zero vendor calls and a finance
actor remains constrained by the agent/run capabilities. Tests also prove
resource and identity-spoofing denials never reach the external service.

This is business logic suitable for invocation from a web handler. It intentionally
does not add routes or modify the existing Pluto example application. Replace
the scripted provider at composition time to use a real model; its scripted
summary demonstrates runtime ergonomics, not live model accuracy.
