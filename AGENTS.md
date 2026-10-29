# Working on Pluto Agent

Keep the root runtime, scripted provider, and OpenAI HTTP adapter stdlib-only.
RBAC and OpenTelemetry dependencies belong in separate integration modules.
Use explicit constructors, context, small interfaces, and typed boundaries.
Never put verified identity, scope, credentials, or clients in model arguments.
Enforce agent registration, run restriction, current permission, and resource
policy at every dispatch. Approval is an additional exact-arguments check.
Never retry a handler or imply durable or exactly-once side effects.
Use the pinned formatter/linter, expanded Go, and the 80-column target. Keep
files below 1,000 lines. Run scripts/fmt.sh, scripts/check.sh, scripts/test.sh,
scripts/fuzz.sh, and scripts/mutation-smoke.py before publishing.
Do not change sibling repositories or run live billable model calls.
