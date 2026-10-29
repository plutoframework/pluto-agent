package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/providers/scripted"
)

func TestCredentialFreePurchaseReviewAndDenials(t *testing.T) {
	var out bytes.Buffer
	if err := demo(&out); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), `"disposition":"needs_review"`) ||
		strings.Count(out.String(), "external calls = 0") != 3 {
		t.Fatalf("%s", out.String())
	}
}

func TestResourceDenialAndModelIdentitySpoofNeverReachExternalAPI(
	t *testing.T,
) {
	for _, raw := range []string{
		`{"id":"vendor-99"}`,
		`{"id":"vendor-7","actor":"lee","scope":"cedar"}`,
	} {
		var calls atomic.Int32

		server := fakeVendors(&calls)

		a, err := build(
			scripted.New(
				scripted.Calls(
					agent.ToolCall{
						ID:        "bad",
						Name:      "read_vendor",
						Arguments: raw,
					},
				),
			),
		)
		if err != nil {
			t.Fatal(err)
		}

		d := Dependencies{
			Actor: agent.Actor{SubjectID: "lee", Scope: "cedar"},
			Purchase: Purchase{
				ID:       "purchase-42",
				TenantID: "cedar",
				VendorID: "vendor-7",
			},
			Vendor: &VendorClient{http: server.Client(), baseURL: server.URL},
		}
		_, err = a.Run(
			context.Background(),
			agent.Run[Dependencies]{ID: "resource-denial", Deps: d},
		)

		server.Close()

		if err == nil || calls.Load() != 0 {
			t.Fatalf("denied request escaped: %v calls=%d", err, calls.Load())
		}

		if raw == `{"id":"vendor-99"}` && !errors.Is(err, agent.ErrDenied) {
			t.Fatalf("resource denial: %v", err)
		}
	}
}
