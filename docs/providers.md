# Provider contract

`Provider` exposes configured-model capabilities and a context-aware `Generate`
method. `StreamingProvider` adds `Stream` with an error-returning delta callback.
The provider-neutral contract has messages, explicit tool calls and correlated
results, finish reasons, usage, output schemas, and runtime events. Providers
receive no dependency object, actor, approval verifier, or authorizer.

The MVP requires native structured output, tool support when tools are
registered, and reported usage. Streaming requires both the streaming interface
and the configured streaming capability. Unsupported capabilities fail at
construction or stream entry, with no prompt-only or non-streaming fallback.
Providers must return one of `final`, `tools`, `length`, or `refusal` and honor
context and emitter failures. Responses transfer ownership to the runtime.
Use the error sentinels with `errors.Is`; inspect OpenAI `HTTPError` with
`errors.As`. Host policy determines whether a new independent run is appropriate.

## OpenAI Chat Completions

The real adapter uses the official `POST /v1/chat/completions` protocol. Select
`gpt-5.4` or snapshot `gpt-5.4-2026-03-05` explicitly. This limited model profile
in non-reasoning mode keeps the first adapter small; support is not inferred
for arbitrary OpenAI-compatible endpoints or model names. Newer model families
requiring other protocols are outside this adapter's capability declaration.

```go
provider, err := openai.New(openai.Config{
    APIKey: credentialFromHostSecretStore,
    Model: "gpt-5.4-2026-03-05",
    HTTPClient: injectedHTTPClient,
})
```

The adapter explicitly sends `reasoning_effort:none`. Other reasoning modes
are outside this initial model profile; no reasoning state is retained.

The adapter sends system instructions, user/assistant history, function calls
and `tool_call_id` results, strict function schemas, and
`response_format:json_schema` for final output. It requests one choice,
`parallel_tool_calls:false`, `store:false`, and `max_completion_tokens`.
Streaming requests `stream_options.include_usage:true` and accumulates indexed
function-argument fragments without executing partial calls. Only complete
responses reach runtime dispatch. SSE must finish with a finish reason, usage,
and `[DONE]`; a disconnected or malformed stream never becomes final output.

Refusals and output truncation are explicit finishes. Unknown finish reasons,
multiple choices, unsupported tool types, missing/invalid usage, malformed
transport JSON, and mixed final-text/tool-call responses error explicitly.
Multimodal content, native/provider-hosted tools, reasoning state, Responses,
parallel execution, log probabilities, and stochastic tuning are deferred.

Default transport limits are 1 MiB total body, 64 KiB accumulated output,
128 KiB SSE line, 8,192 chunks, and 128 accumulated tool calls. The runtime
applies its own smaller tool-call and accepted-output limits as well. The
constructor caps configurable body/output limits at 16 MiB/1 MiB. A default
HTTP client has a 45-second timeout; an injected client retains its timeout
and transport. Runtime contexts add per-call deadlines.

HTTPS is required. Plain HTTP is allowed only with explicit opt-in and an IP
literal loopback host, for local mocks. Endpoint credentials, query strings,
fragments, and redirects are rejected. Custom HTTPS endpoints are trusted host
configuration and receive the key. The client is copied before redirects are
disabled; caller client settings are not modified. A custom transport remains
a trusted credential-handling boundary. API keys are explicit constructor
inputs, not read from global environment variables by the library.

HTTP errors contain status codes and advisory retryability for 429/5xx, not
response bodies or credentials. Network errors are generic. Neither the adapter
nor runtime retries them. Mock contract tests validate requests, tool-result
correlation, text/tool SSE, usage, cancellation, body limits, errors, malformed
protocols, and redirect credential isolation. These are local protocol tests;
**no live model call was made** and account/model availability was not tested.

Official sources inspected for this implementation:

- [Chat Completions create reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
- [Structured outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
- [Function calling](https://developers.openai.com/api/docs/guides/function-calling)
- [Streaming](https://developers.openai.com/api/docs/guides/streaming-responses)
- [GPT-5.4 capabilities](https://developers.openai.com/api/docs/models/gpt-5.4)

## Scripted provider

`scripted.New` consumes deterministic `Step` values. `Calls` and `Final` build
common steps; `Check` inspects an isolated request before returning a response.
`Deltas` exercises streaming, and `Err` injects failures. Sequence consumption
is mutex protected. Create one provider per run for a run-specific script;
concurrent runs sharing one sequence can interleave steps. It retains no input
history and makes no network calls. Tests of this provider establish runtime
behavior, not stochastic model accuracy or resistance to every prompt attack.

PydanticAI inspiration comes from its official
[dependencies](https://pydantic.dev/docs/ai/core-concepts/dependencies/),
[output](https://pydantic.dev/docs/ai/core-concepts/output/), and
[testing](https://pydantic.dev/docs/ai/guides/testing/) documentation. This Go
library implements a deliberately narrower application-oriented contract.
