# OpenTelemetry observer

Module: `github.com/plutoframework/pluto-agent/integrations/otel v0.1.0`.
Call `otel.New(injectedTracer)` and supply the returned observer through
`agent.Config.Observer`. `pluto-core/telemetry v0.1.1` exposes a compatible
`Runtime.Tracer`; any OTEL tracer works. Exporter, SDK, shutdown, and sampling
configuration belong to the host. No global provider is installed.

Spans have fixed names `agent.run`, `agent.model`, and `agent.tool` and preserve
context parenting. Attributes are a failure flag and known input/output token
counts clamped to `[0, 1_000_000_000]`. No tool/model names, prompt/input/output,
arguments, credentials, actor/tenant/run IDs, or raw errors are recorded.
Failure status uses a fixed generic message. End callbacks are idempotent.
Usage can be incomplete on failed provider calls.

The runtime dependency is the trace API. The optional module's SDK requirement
supports in-memory span tests. The root agent module stays stdlib-only.
Observational spans do not replace durable authorization/approval audit storage.
