# Security and ownership boundaries

The host owns authentication and tenant selection. Extract a principal from
verified server/session context into typed run dependencies. Do not populate
`Actor`, a client endpoint, credentials, authorizer, approval verifier, or run ID
from a model response or unverified JSON/header. The library cannot establish
the authenticity of arbitrary Go values supplied by a caller.

`Principal` snapshots the verified actor/scope at run start. Permission resolution
is fresh at every permissioned tool dispatch, including repeated reads. Use a
live resolver when revocation must take effect within a run. The optional
`plutorbac` adapter delegates exact subject/scope/permission checks to the
published RBAC package; it has no grant algorithm, role fallback, or cache.
Static assignments in the example are explicitly immutable demo fixtures.

The agent's tool set is fixed at construction. Run restrictions are immutable
copies and can only narrow it. All batch routing and duplicate call IDs are
checked before dispatching any member. Schema filtering is an exposure reduction,
not authorization. A malicious or stale model can request unadvertised names;
dispatch still denies them. Invalid tool arguments never reach a handler.

Every tool declares public or permissioned access, an effect class, and a
resource policy. Permission success does not establish resource ownership. The
policy checks the actual validated resource against host-selected tenant scope
and application rules. Public tools still require this policy, including when
the host deliberately permits an anonymous actor. Do not classify a write tool
as read-only to obtain retry or replay behavior.

Approval is additional to permission and resource policy. Even public tools
require a host-verified nonempty subject when approval is required. A verifier
must match run ID, tool ID, exact canonical validated JSON, and verified actor
and scope against a host-approved record. The supplied hash covers the JSON;
it is a storage aid, not a signature or permission. Persistence, approver
authentication, expiry, revocation, atomic consumption/reuse, and transaction
boundaries belong to the host. No prompt boolean or tool argument is approval.
The MVP supplies no approval service or storage, and the example never approves
or pays anything.

Checks and execution are separate calls; resource state and grants can change
between them. For a sensitive operation, the business service must enforce its
own current authorization/resource invariants at the transaction or external
service boundary. Policies/verifiers are trusted code and should not perform
unauthorized writes. The library supplies no atomic authorization transaction.

No handler, HTTP request, or whole run is automatically retried. Validation
repairs occur only before tool execution or for final output. Handlers must
return errors for uncertain effects; a canceled request does not prove that an
external service rolled back. Duplicate call IDs and repeated canonical write
arguments are rejected only within an in-memory run. Different arguments may
still describe equivalent effects. A new run, process crash, queue redelivery,
or host retry can repeat an action. Use business idempotency and durable audit
storage where required; jobs retain at-least-once semantics. No exactly-once or
durable recovery claim is made.

Limits bound accepted/retained JSON and attempt counts, and set context deadlines.
Byte checks after serialization do not cap allocations inside a handler,
validator, custom transport, or provider. Context cannot forcibly interrupt
noncooperative Go code. Caller-injected interfaces must honor context and be
safe for concurrent use when shared. Providers transfer response ownership to
the runtime and must obey transport limits; requests are isolated copies.

Token counts come from completed provider responses. The runtime stops after
reported usage crosses `MaxTokens`, and requests at most `MaxOutputTokens`
(also narrowed by the remaining accounting ceiling) per model call. Request
tokens are not estimated before sending. A call may overshoot the cumulative
ceiling; interrupted responses can lack usage. `Known=false` marks incomplete
aggregate accounting, while totals retain earlier reported calls. Do not treat
these counters as hard spend limits.

Partial stream text, tool results, provider errors, and final summaries can
contain sensitive or adversarial data. Keep rendering/escaping and HTTP error
mapping in the host. Do not expose raw handler/authorizer errors to clients.
The OpenAI transport intentionally returns status-only HTTP errors and generic
network errors. A custom endpoint and transport are trusted configuration and
receive the credential; redirects are disabled. `store:false` is an API request
setting, not a promise about provider retention policy.

The observer seam exposes fixed operation kinds, a failure flag, and usage,
not input, prompt, tool arguments/results, credentials, actor/tenant/run IDs,
or arbitrary error text. Optional OTEL spans are observational and do not
replace durable audit records. Streaming partials are an explicit payload API,
so the consuming host must apply its own access and retention policy.
