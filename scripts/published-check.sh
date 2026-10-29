#!/usr/bin/env bash
set -euo pipefail
scratch=$(mktemp -d)
trap 'chmod -R u+w "$scratch"; rm -rf "$scratch"' EXIT
export GOWORK=off
export GOMODCACHE="$scratch/modcache"
export GOCACHE="$scratch/buildcache"
cd "$scratch"
go mod init example.com/pluto-agent-consumer
go get github.com/plutoframework/pluto-agent@v0.1.1 \
  github.com/plutoframework/pluto-agent/integrations/plutorbac@v0.1.1 \
  github.com/plutoframework/pluto-agent/integrations/otel@v0.1.1
cat > consumer.go <<'GO'
package consumer

import (
    "context"

    agent "github.com/plutoframework/pluto-agent"
    "github.com/plutoframework/pluto-agent/providers/scripted"
)

type Summary struct { Text string `json:"text"` }

func Summarize(ctx context.Context) (Summary, error) {
    a, err := agent.New(agent.Config[struct{}, Summary]{
        Provider: scripted.New(scripted.Final(`{"text":"fresh consumer"}`)),
        Principal: func(struct{}) (agent.Actor, error) { return agent.Actor{}, nil },
    })
    if err != nil { return Summary{}, err }
    result, err := a.Run(ctx, agent.Run[struct{}]{ID: "fresh-build"})
    return result.Output, err
}
GO
cat > consumer_test.go <<'GO'
package consumer_test

import (
    "context"
    "testing"

    consumer "example.com/pluto-agent-consumer"

    agent "github.com/plutoframework/pluto-agent"
    agentotel "github.com/plutoframework/pluto-agent/integrations/otel"
    "github.com/plutoframework/pluto-agent/integrations/plutorbac"
    "github.com/plutoframework/pluto-agent/providers/openai"
    "github.com/plutoframework/pluto-agent/providers/scripted"

    rbac "github.com/plutoframework/pluto-rbac"
    "go.opentelemetry.io/otel/trace/noop"
)

type checker struct{}

func (checker) Authorize(context.Context, rbac.Request) error {
    return rbac.ErrForbidden
}

func TestPublishedConsumer(t *testing.T) {
    summary, err := consumer.Summarize(context.Background())
    if err != nil || summary.Text != "fresh consumer" { t.Fatal(summary, err) }
    type Output struct { Summary string `json:"summary"` }
    authorizer, err := plutorbac.New(checker{})
    if err != nil { t.Fatal(err) }
    observer, err := agentotel.New(noop.NewTracerProvider().Tracer("consumer"))
    if err != nil { t.Fatal(err) }
    a, err := agent.New(agent.Config[struct{}, Output]{
        Provider: scripted.New(scripted.Final(`{"summary":"downloaded"}`)),
        Principal: func(struct{}) (agent.Actor, error) { return agent.Actor{}, nil },
        Authorizer: authorizer,
        Observer: observer,
    })
    if err != nil { t.Fatal(err) }
    result, err := a.Run(context.Background(), agent.Run[struct{}]{ID: "fresh-consumer"})
    if err != nil || result.Output.Summary != "downloaded" {
        t.Fatalf("%+v %v", result, err)
    }
    // Construct only; do not contact any live provider or use an actual key.
    if _, err := openai.New(openai.Config{
        APIKey: "synthetic-unused-credential", Model: "gpt-5.4",
    }); err != nil { t.Fatal(err) }
}
GO
go mod tidy
go list -m -json all > modules.json
python3 - <<'PY'
import json
from pathlib import Path
raw = Path('modules.json').read_text()
decoder = json.JSONDecoder()
while raw.strip():
    value, end = decoder.raw_decode(raw.lstrip())
    if 'Replace' in value:
        raise SystemExit('Published consumer unexpectedly used a replacement')
    raw = raw.lstrip()[end:]
PY
go test ./...
go build ./...
go run github.com/plutoframework/pluto-agent/examples/purchase-review@v0.1.1
