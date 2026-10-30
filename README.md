<p align="center">
  <img src="https://raw.githubusercontent.com/plutoframework/pluto/main/assets/readme/banner.svg" alt="Pluto — a small framework, wide open possibilities" width="100%" />
</p>

# Pluto Agent

Typed AI features with server-owned authority.

[Website](https://plutoframework.github.io/pluto-site/) · [Documentation](https://plutoframework.github.io/pluto-site/docs/backend/agents/) · [CLI demo](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md) · [First issues](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks)

## What you get

- Typed dependencies, structured output and explicitly registered tools.
- Application operations and external API tools share policy boundaries.
- Scripted providers make denied paths and business behavior testable.

## Get started

### 1. Install

From a Go **1.25.5+** module:

```sh
go get github.com/plutoframework/pluto-agent@v0.1.3
```

The root module uses only the standard library. RBAC and OTEL integrations are
separate modules; Pluto is optional.

### 2. Run a key-free review

```sh
go run github.com/plutoframework/pluto-agent/examples/purchase-review@v0.1.2
```

This uses a scripted model and a synthetic loopback vendor service. It makes no
paid model calls. Successful review and denied scenarios exercise typed tools;
denied runs must produce zero external calls. See the [example](examples/purchase-review/README.md).

### 3. See the typed tool and its resource policy

The runnable review example includes [typed dependencies](examples/purchase-review/domain.go),
[RBAC wiring](examples/purchase-review/domain.go), [tool definitions](examples/purchase-review/tools.go)
and [the full runner](examples/purchase-review/main.go). This exact tool definition
comes from `reviewTools()`; its types and setup are in those complete sources:

<!-- example-excerpt: examples/purchase-review/tools.go:purchase -->

```go
purchase, err := agent.NewTool(
	agent.ToolConfig[Dependencies, IDArgs, Purchase]{
		Name:        "read_purchase",
		Description: "Read the purchase under review.",
		Access: agent.Access{
			Permission: "purchases:read",
		},
		Effect:   agent.ReadOnly,
		Validate: validID,
		Policy: func(_ context.Context, d Dependencies, actor agent.Actor, a IDArgs) error {
			if a.ID != d.Purchase.ID || d.Purchase.TenantID != actor.Scope {
				return agent.ErrDenied
			}

			return nil
		},
		Handle: func(_ context.Context, d Dependencies, _ IDArgs) (Purchase, error) {
			return d.Purchase, nil
		},
	},
)
```
<!-- /example-excerpt -->

The model supplies an ID; verified identity and the selected purchase come from
server dependencies. A permitted tool still checks the actual resource scope.
The vendor tool follows the same pattern with an injected HTTP client.

The scripted runner calls `read_purchase`, `read_vendor` and `read_policy`, then
returns a typed review. It also exercises denied actor/run/agent restrictions;
each denied scenario reports **external calls = 0**. This verifies tool authority,
not live-model accuracy. [Tests](examples/purchase-review/main_test.go) cover denied
resource access and identity spoofing as well.

### 4. Connect your own capabilities

Inject verified identity, tenant scope, clients and credentials through server-owned
dependencies. Model arguments supply business inputs, never authority. Registered
tools call authorized operations or explicitly permitted external APIs. Narrow the
run's tool set and keep approvals bound to exact arguments where required.

## Read further

- [API and integration reference](docs/REFERENCE.md)
- [Field guide](https://plutoframework.github.io/pluto-site/docs/backend/agents/)
- [Pluto ecosystem](https://github.com/plutoframework/pluto#the-ecosystem)

## Boundaries

MVP provider support is intentionally narrow; use the released provider reference. Runs/replay guards are in memory, cancellation is cooperative and accounting is not a hard billing cap. Approval persistence and durable recovery remain host-owned.

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md), then choose a [good first issue](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks).
