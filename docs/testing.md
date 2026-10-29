# Validation

Run `scripts/fmt.sh`, `scripts/check.sh`, `scripts/test.sh`, `scripts/fuzz.sh`,
and `python3 scripts/mutation-smoke.py`. Formatting is mutating; the check
command is nonmutating for source. Tests use no cloud keys, database, paid model,
or fixed listener port. The local vendor service uses `httptest`.

`scripts/test.sh` runs `go test -race ./...` in all four modules: root, RBAC
integration, OTEL integration, and purchase-review example. Extra Go test flags
can be passed through. Root and provider packages contain deterministic tests;
optional integrations test their published dependencies and actual span data.
Go examples are compiled and output assertions executed.

The suite exercises independent registration/access, run restriction, current
permission, resource, and approval expectations. Denied handlers/external
clients have asserted zero calls. Tests include public-vs-undeclared access,
empty/unknown restrictions, privilege narrowing, malformed/unknown tool args,
business validators, exact approval bindings, revocation between repeated calls,
call-ID/write replay, bounded repairs and cumulative limits, deadlines, canceled
and invalid streams, failed-provider accounting, concurrent runs, immutable
history/schema boundaries, HTTP errors, and non-retried uncertain effects.

The fuzz script runs three bounded targets with two workers each: schema
decoding, dispatch with at least one closed authorization gate, and SSE decoding.
The default is three seconds per target; `FUZZTIME=10s ./scripts/fuzz.sh`
increases the bound. Fuzzing provides sampled coverage, not exhaustive proof.

The mutation smoke uses seed `20261028` and 12 explicit mutants spanning
registration, run restrictions, permission, resource policy, approval, write
replay, input/model/tool/token/repair limits, and object validation. It copies
only this module into temporary directories, compiles each mutant separately,
and then requires the independent tests to fail. Build failures, invalid anchors,
and timeouts are errors and never count as killed mutants. It does not modify
the working source or sibling repositories.

`scripts/published-check.sh` creates a fresh module/cache, downloads all four
published v0.1.1 modules with `GOWORK=off`, checks for replacements, builds and
tests a consumer, and executes the published example. This validates publication
and ergonomic consumption without relying on a developer workspace.

Stochastic task-quality evaluation, live-provider compatibility and availability,
long-running load/latency measurements, dollar accounting, and an independent
security audit are outside these checks. No benchmark/performance claim is made.
