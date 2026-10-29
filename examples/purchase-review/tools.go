package main

import (
	"context"
	"errors"
	"regexp"

	agent "github.com/plutoframework/pluto-agent"
)

type IDArgs struct {
	ID string `json:"id"`
}

type NoArgs struct{}

var resourceID = regexp.MustCompile(`^[a-z]+-[0-9]+$`)

func validID(_ context.Context, _ Dependencies, args IDArgs) error {
	if !resourceID.MatchString(args.ID) {
		return errors.New("invalid resource ID")
	}

	return nil
}

func reviewTools() ([]agent.Tool[Dependencies], error) {
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
	if err != nil {
		return nil, err
	}

	vendor, err := agent.NewTool(agent.ToolConfig[Dependencies, IDArgs, Vendor]{
		Name:        "read_vendor",
		Description: "Look up this purchase's vendor through the injected API client.",
		Access: agent.Access{
			Permission: "vendors:read",
		},
		Effect:   agent.ReadOnly,
		Validate: validID,
		Policy: func(_ context.Context, d Dependencies, actor agent.Actor, a IDArgs) error {
			if d.Vendor == nil || a.ID != d.Purchase.VendorID ||
				d.Purchase.TenantID != actor.Scope {
				return agent.ErrDenied
			}

			return nil
		},
		Handle: func(ctx context.Context, d Dependencies, a IDArgs) (Vendor, error) {
			return d.Vendor.Get(ctx, a.ID)
		},
	})
	if err != nil {
		return nil, err
	}

	policy, err := agent.NewTool(agent.ToolConfig[Dependencies, NoArgs, Policy]{
		Name:        "read_policy",
		Description: "Read the host-selected organization review policy.",
		Access:      agent.Access{Permission: "policy:read"},
		Effect:      agent.ReadOnly,
		Policy: func(_ context.Context, d Dependencies, actor agent.Actor, _ NoArgs) error {
			if d.Purchase.TenantID != actor.Scope {
				return agent.ErrDenied
			}

			return nil
		},
		Handle: func(_ context.Context, d Dependencies, _ NoArgs) (Policy, error) {
			return d.Policy, nil
		},
	})
	if err != nil {
		return nil, err
	}

	return []agent.Tool[Dependencies]{purchase, vendor, policy}, nil
}
