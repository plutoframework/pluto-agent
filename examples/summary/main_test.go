package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/openai"
)

func TestSummaryUsesRealAdapter(t *testing.T) {
	calls := 0

	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		calls++

		var request struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Tools []json.RawMessage `json:"tools"`
		}

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}

		if request.Model != "gpt-5.4" ||
			request.ResponseFormat.Type != "json_schema" ||
			len(request.Tools) != 0 {
			t.Error("expected typed OpenAI summary with no tools")
		}

		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write(
			[]byte(
				`{"choices":[{"message":{"role":"assistant","content":"{\"text\":\"Shipment is delayed.\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":5}}`,
			),
		)
	}))
	defer server.Close()

	provider, err := openai.New(openai.Config{
		APIKey:                "synthetic-test-credential",
		Model:                 "gpt-5.4",
		Endpoint:              server.URL,
		HTTPClient:            server.Client(),
		AllowInsecureLoopback: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	text, err := summarize(
		context.Background(),
		provider,
		agent.Actor{SubjectID: "test-operator"},
		"The shipment will arrive two days late.",
	)
	if err != nil {
		t.Fatal(err)
	}

	if text != "Shipment is delayed." || calls != 1 {
		t.Fatalf("unexpected summary %q with %d calls", text, calls)
	}
}
