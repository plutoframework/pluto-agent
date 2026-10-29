package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	agent "github.com/plutoframework/pluto-agent"
	"github.com/plutoframework/pluto-agent/integrations/plutorbac"

	rbac "github.com/plutoframework/pluto-rbac"
)

type Purchase struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	VendorID    string `json:"vendor_id"`
	AmountCents int    `json:"amount_cents"`
}

type Vendor struct {
	Name     string `json:"name"`
	Standing string `json:"standing" enum:"approved,hold"`
}

type Policy struct {
	ReviewAboveCents int `json:"review_above_cents"`
}

type Review struct {
	Summary     string   `json:"summary"`
	Disposition string   `json:"disposition" enum:"routine,needs_review"`
	NextSteps   []string `json:"next_steps"`
}

// Dependencies come from application composition and verified server context.
// None of these fields are decoded from the model's tool arguments.
type Dependencies struct {
	Actor    agent.Actor
	Purchase Purchase
	Vendor   *VendorClient
	Policy   Policy
}

type VendorClient struct {
	http    *http.Client
	baseURL string
}

func (v *VendorClient) Get(ctx context.Context, id string) (Vendor, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		v.baseURL+"/vendors/"+id,
		nil,
	)
	if err != nil {
		return Vendor{}, err
	}

	response, err := v.http.Do(req)
	if err != nil {
		return Vendor{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Vendor{}, errors.New("vendor lookup failed")
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return Vendor{}, errors.New("invalid vendor response")
	}

	schema, err := agent.SchemaFor[Vendor]()
	if err != nil {
		return Vendor{}, err
	}

	var vendor Vendor
	if err := schema.Decode(raw, 4096, &vendor); err != nil {
		return Vendor{}, err
	}

	return vendor, nil
}

// Synthetic external service: no cloud, API key, money movement, or port 8080.
func fakeVendors(calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)

			if r.Method != http.MethodGet || r.URL.Path != "/vendors/vendor-7" {
				http.NotFound(w, r)

				return
			}

			w.Header().Set("Content-Type", "application/json")

			_ = json.NewEncoder(w).
				Encode(Vendor{Name: "Cedar Labs", Standing: "approved"})
		}),
	)
}

func permissions() (*plutorbac.Authorizer, error) {
	policy, err := rbac.New(rbac.Definition{
		Permissions: []rbac.Permission{
			{
				ID:          "purchases:read",
				Name:        "purchases:read",
				Description: "Synthetic purchase review capability or role.",
			},
			{
				ID:          "vendors:read",
				Name:        "vendors:read",
				Description: "Synthetic purchase review capability or role.",
			},
			{
				ID:          "policy:read",
				Name:        "policy:read",
				Description: "Synthetic purchase review capability or role.",
			},
			{
				ID:          "purchases:pay",
				Name:        "purchases:pay",
				Description: "Synthetic purchase review capability or role.",
			},
		},
		Roles: []rbac.Role{
			{
				ID:          "viewer",
				Name:        "viewer",
				Description: "Synthetic purchase review capability or role.",
				Permissions: []rbac.PermissionID{
					"purchases:read",
					"policy:read",
				},
			},
			{
				ID:          "reviewer",
				Name:        "reviewer",
				Description: "Synthetic purchase review capability or role.",
				Inherits:    []rbac.RoleID{"viewer"},
				Permissions: []rbac.PermissionID{"vendors:read"},
			},
			{
				ID:          "finance",
				Name:        "finance",
				Description: "Synthetic purchase review capability or role.",
				Inherits:    []rbac.RoleID{"reviewer"},
				Permissions: []rbac.PermissionID{"purchases:pay"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	resolver, err := rbac.NewStaticResolver([]rbac.Assignment{
		{
			SubjectID: "riley",
			Scope:     rbac.Scope{TenantID: "cedar"},
			Roles:     []rbac.RoleID{"reviewer"},
		},
		{
			SubjectID: "sam",
			Scope:     rbac.Scope{TenantID: "cedar"},
			Roles:     []rbac.RoleID{"viewer"},
		},
		{
			SubjectID: "lee",
			Scope:     rbac.Scope{TenantID: "cedar"},
			Roles:     []rbac.RoleID{"finance"},
		},
	})
	if err != nil {
		return nil, err
	}

	checker, err := rbac.NewAuthorizer(
		policy,
		resolver,
		rbac.Options{RequireTenant: true},
	)
	if err != nil {
		return nil, err
	}

	return plutorbac.New(checker)
}
