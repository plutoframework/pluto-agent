package agent_test

import (
	"context"
	"fmt"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func ExampleAgent_Run() {
	type Dependencies struct{ Actor agent.Actor }

	type Summary struct {
		Text string `json:"text"`
	}

	a, err := agent.New(agent.Config[Dependencies, Summary]{
		Provider:     scripted.New(scripted.Final(`{"text":"Reviewed."}`)),
		Instructions: "Summarize the supplied text.",
		Principal:    func(d Dependencies) (agent.Actor, error) { return d.Actor, nil },
	})
	if err != nil {
		panic(err)
	}

	// The host authenticated this actor before invoking application logic.
	verifiedActor := agent.Actor{SubjectID: "reviewer", Scope: "organization"}

	result, err := a.Run(context.Background(), agent.Run[Dependencies]{
		ID:    "host-generated-run-id",
		Deps:  Dependencies{Actor: verifiedActor},
		Input: "Text supplied by the application.",
		Tools: agent.OnlyTools(),
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Status, result.Output.Text)

	// Output: completed Reviewed.
}
