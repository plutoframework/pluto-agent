# Pluto Agent

Typed, testable AI-backed Go application features: summarization,
classification, extraction, and explicitly registered tools. The root module
uses only the Go standard library. Pluto is optional.

This is an MVP inspired by PydanticAI's typed dependencies, tools, output
validation, and deterministic testing. It is an application library, with a
small supported schema subset and explicit ownership boundaries.

## Install

Go 1.25.5 or newer:

```sh
go get github.com/plutoframework/pluto-agent@v0.1.1
```

Optional modules are independently pinned:

```sh
go get github.com/plutoframework/pluto-agent/integrations/plutorbac@v0.1.1
go get github.com/plutoframework/pluto-agent/integrations/otel@v0.1.1
```

The RBAC adapter uses published `pluto-rbac v0.1.0`. The telemetry adapter uses
OpenTelemetry's trace API and accepts `pluto-core/telemetry v0.1.1`'s `Tracer`.
It does not configure an SDK or exporter. Its SDK dependency is used by tests.

## A typed run

```go
type Dependencies struct {
    Actor agent.Actor // Extracted from verified server context.
}

type Summary struct {
    Text string `json:"text"`
}

a, err := agent.New(agent.Config[Dependencies, Summary]{
    Provider: scripted.New(scripted.Final(`{"text":"Reviewed."}`)),
    Instructions: "Summarize the supplied text.",
    Principal: func(d Dependencies) (agent.Actor, error) {
        return d.Actor, nil
    },
})
if err != nil {
    return err
}
result, err := a.Run(ctx, agent.Run[Dependencies]{
    ID: "host-generated-run-id",
    Deps: Dependencies{Actor: verifiedActor},
    Input: "Text supplied by the application.",
    Tools: agent.OnlyTools(), // Explicitly deny every tool for this run.
})
```

Imports are `agent "github.com/plutoframework/pluto-agent"` and
`"github.com/plutoframework/pluto-agent/providers/scripted"`. A complete version
is compiled in [example_test.go](example_test.go).

Construct tools with `NewTool(ToolConfig[D, Args, Result]{...})`, then pass the
resulting `[]Tool[D]` to `New`. `Handle` is an ordinary
`func(context.Context, D, Args) (Result, error)`: it can call an existing Pluto
operation or any injected client. No HTTP endpoint is required. HTTP request
decoding and authentication stay in the host application.

## Purchase review, without credentials

```sh
go run github.com/plutoframework/pluto-agent/examples/purchase-review@v0.1.1
```

Or, from this checkout:

```sh
./scripts/example.sh
```

The [synthetic example](examples/purchase-review) reviews a $1,250 diagnostic
services purchase from Cedar Labs. Three typed tools read a purchase, vendor,
and organization policy. The vendor tool calls an injected local HTTP client
and fake service on a random loopback port. The scripted provider checks the
actual tool-result history before returning a structured summary and suggested
human next steps. It performs no approval, payment, or cloud calls.

It also demonstrates RBAC denial with **zero external calls**, and a finance
actor whose broader RBAC capabilities cannot widen either the agent's registered
tools or the current run's narrower tool set. Resource and identity-spoofing
denials are covered in the example's tests.

## Authorization and approval

Every actual tool dispatch must satisfy all four gates:

1. The tool belongs to this agent's explicit registration set.
2. The run allows it. The zero `Restriction` allows the registered set;
   `OnlyTools()` denies all. Unknown/out-of-agent restriction IDs error before
   any model call. A restriction can only narrow.
3. For permissioned tools, an injected authorizer checks the verified actor's
   current permission on every dispatch. No decision cache is authoritative.
4. A mandatory resource `Policy` accepts the actual validated arguments.

`Access{}` fails registration. Public access requires `Access{Public: true}`;
it skips the permission check but still requires run and resource gates. Use
the published [RBAC adapter](integrations/plutorbac) rather than writing a
second grant algorithm. The provider receives schemas filtered by the run;
dispatch checks remain authoritative even if the model requests a hidden tool.

Identity, tenant scope, credentials, and clients are supplied through typed
dependencies and host construction. They are never decoded from model tool
arguments. An `Actor` value itself is not proof of authentication: the host
must verify it before invoking the library.

`ApprovalRequired` adds an injected `ApprovalVerifier` check after permission
and resource policy. Missing or rejecting verifiers fail closed. The request
includes run ID, tool ID, verified actor/scope, canonical validated exact
arguments, and their SHA-256 hash. The host owns approval authentication,
expiration, persistence, atomic consumption, and reuse policy. The hash is not
an approval token. Approval never grants a missing permission.

See [security and ownership](docs/security.md) for the full host contract.

## Validation, limits, and events

Schemas support explicit structs, strings, booleans, integers, floats, and
non-null slices within a small documented subset. Required fields, exact field
names, unknown fields, types, enum tags, duplicate keys, nulls, ranges, JSON depth,
and byte size are checked before Go decoding. Use `Validate`, `ValidateResult`,
and `ValidateOutput` for business constraints. Unsupported types fail during
construction. See [schema support](docs/schema.md).

The loop returns typed output, completion status, usage, and attempt counts.
It bounds input/request/response/tool-result bytes, model/tool attempts, repair
attempts, and elapsed time through context. All limits have finite defaults.
`MaxTokens` checks reported input plus output tokens **after** each model call.
`MaxOutputTokens` is forwarded to the provider. These are not hard dollar or
preflight token guarantees. Failed streams may have incomplete usage.

Only argument validation before execution and final-output validation request
bounded model repairs. Permission/resource/approval failures, handler failures,
and provider errors stop the run. No automatic HTTP, tool, or whole-run retries.
Read tools may repeat with fresh authorization; duplicate call IDs and repeated
side-effect tool/argument pairs are rejected within a run. This is not durable
deduplication across runs.

`RunStream(ctx, run, emit)` supplies synchronous backpressure. `Stream(ctx, run)`
offers an unbuffered `Events` channel, `Close`, and `Wait`. Drain the channel or
cancel/close the stream before waiting. `partial_unvalidated` text can be
malformed or belong to an attempt later repaired. Only `final_validated` carries
typed output. Consumers must handle cancellation and delivery errors.

## Provider support

`providers/openai` is a real, injectable stdlib HTTP implementation of OpenAI
Chat Completions for `gpt-5.4` and `gpt-5.4-2026-03-05`. It explicitly sets
`reasoning_effort:none` and implements strict
structured output, function tools, reported usage, and SSE streaming. Unsupported
models/capabilities/shapes error explicitly. It sends `store:false`, disables
redirects, bounds transport bytes, and omits response bodies and URLs from
transport errors. No live provider call was made to validate this release.

[Provider details and official protocol references](docs/providers.md) document
the deliberately narrow compatibility contract. The deterministic scripted
provider tests runtime behavior; it does not measure model quality.

## Development

```sh
mise trust
mise install
./scripts/fmt.sh
./scripts/check.sh
./scripts/test.sh
./scripts/fuzz.sh
python3 scripts/mutation-smoke.py
```

The formatter/linter is pinned to `golangci-lint 2.15.0`, with the canonical
gci/gofumpt/golines/wsl_v5 configuration and 80-column target. Files stay below
1,000 lines. Development scripts create a temporary workspace; committed module
files have published pins and no local replacements.

[Validation details](docs/testing.md) distinguish deterministic correctness,
bounded fuzz/mutation checks, and deferred stochastic evaluations. These checks
do not constitute an independent security audit.

## Deferred

Multi-agent orchestration, vector stores/RAG, memory, shell/browser tools, remote
plugins, a durable/checkpoint engine, automatic tool retries, approval storage,
provider parity, broad schema support, CLI scaffolding, and frontend integration
are outside this release. Queue/job integration would retain at-least-once
semantics and cannot establish exactly-once external effects by itself.
