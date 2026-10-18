// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package negotiate

import (
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

func cap(name, ver string, extends ...string) (string, wire.Capability) {
	c := wire.Capability{
		Version: wire.Version(ver),
		Spec:    "https://ucp.dev/spec",
		Schema:  "https://ucp.dev/schema.json",
	}
	if len(extends) > 0 {
		c.Extends = wire.NewExtends(extends...)
	}
	return name, c
}

func profileWith(version string, caps ...struct {
	name string
	cap  wire.Capability
}) *wire.Profile {
	p := &wire.Profile{
		UCP: wire.UCP{
			Version:      wire.Version(version),
			Capabilities: map[string][]wire.Capability{},
		},
	}
	for _, c := range caps {
		p.UCP.Capabilities[c.name] = append(p.UCP.Capabilities[c.name], c.cap)
	}
	return p
}

type entry = struct {
	name string
	cap  wire.Capability
}

func entryFor(name, ver string, extends ...string) entry {
	_, c := cap(name, ver, extends...)
	return entry{name: name, cap: c}
}

func TestIntersect(t *testing.T) {
	tests := []struct {
		name     string
		business *wire.Profile
		platform *wire.Profile
		want     []string // expected capability names in result
	}{
		{
			name: "single matching root capability",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
			),
			platform: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
			),
			want: []string{"dev.ucp.shopping.checkout"},
		},
		{
			name: "no common capabilities",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
			),
			platform: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.cart", "2026-04-08"),
			),
			want: []string{},
		},
		{
			name: "no mutual version excludes the capability",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
			),
			platform: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2025-01-01"),
			),
			want: []string{},
		},
		{
			name: "picks highest mutual version",
			business: &wire.Profile{
				UCP: wire.UCP{
					Version: "2026-04-08",
					Capabilities: map[string][]wire.Capability{
						"dev.ucp.shopping.checkout": {
							{Version: "2026-04-08", Spec: "x", Schema: "y"},
							{Version: "2026-01-01", Spec: "x", Schema: "y"},
							{Version: "2025-06-01", Spec: "x", Schema: "y"},
						},
					},
				},
			},
			platform: &wire.Profile{
				UCP: wire.UCP{
					Version: "2026-04-08",
					Capabilities: map[string][]wire.Capability{
						"dev.ucp.shopping.checkout": {
							{Version: "2026-01-01", Spec: "x", Schema: "y"},
							{Version: "2025-06-01", Spec: "x", Schema: "y"},
						},
					},
				},
			},
			want: []string{"dev.ucp.shopping.checkout"},
		},
		{
			name: "extension is kept when parent present",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.fulfillment", "2026-04-08", "dev.ucp.shopping.checkout"),
			),
			platform: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.fulfillment", "2026-04-08", "dev.ucp.shopping.checkout"),
			),
			want: []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.fulfillment"},
		},
		{
			name: "orphan extension is pruned",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.fulfillment", "2026-04-08", "dev.ucp.shopping.checkout"),
			),
			platform: profileWith("2026-04-08",
				// platform does NOT advertise checkout, only fulfillment
				entryFor("dev.ucp.shopping.fulfillment", "2026-04-08", "dev.ucp.shopping.checkout"),
			),
			want: []string{}, // fulfillment is orphaned because checkout missing
		},
		{
			name: "multi-parent extension kept when at least one parent present",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.discount", "2026-04-08",
					"dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"),
			),
			platform: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.discount", "2026-04-08",
					"dev.ucp.shopping.checkout", "dev.ucp.shopping.cart"),
			),
			want: []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.discount"},
		},
		{
			name: "transitive orphan pruning",
			business: profileWith("2026-04-08",
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("dev.ucp.shopping.fulfillment", "2026-04-08", "dev.ucp.shopping.checkout"),
				entryFor("com.example.extra_shipping", "2026-04-08", "dev.ucp.shopping.fulfillment"),
			),
			platform: profileWith("2026-04-08",
				// platform doesn't advertise fulfillment, so extra_shipping
				// must be transitively pruned even though its direct parent
				// fulfillment was initially included from business side.
				entryFor("dev.ucp.shopping.checkout", "2026-04-08"),
				entryFor("com.example.extra_shipping", "2026-04-08", "dev.ucp.shopping.fulfillment"),
			),
			want: []string{"dev.ucp.shopping.checkout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Intersect(tt.business, tt.platform)
			gotNames := got.Names()
			if len(gotNames) != len(tt.want) {
				t.Fatalf("got %d caps %v, want %d %v", len(gotNames), gotNames, len(tt.want), tt.want)
			}
			for i, name := range tt.want {
				if gotNames[i] != name {
					t.Errorf("cap[%d] = %q, want %q", i, gotNames[i], name)
				}
			}
		})
	}
}

func TestFilterResponseCapabilities(t *testing.T) {
	result := Result{
		Capabilities: map[string]wire.Capability{
			"dev.ucp.shopping.checkout":    {Version: "2026-04-08"},
			"dev.ucp.shopping.cart":        {Version: "2026-04-08"},
			"dev.ucp.shopping.order":       {Version: "2026-04-08"},
			"dev.ucp.shopping.fulfillment": {Version: "2026-04-08", Extends: wire.NewExtends("dev.ucp.shopping.checkout")},
			"dev.ucp.shopping.discount":    {Version: "2026-04-08", Extends: wire.NewExtends("dev.ucp.shopping.checkout", "dev.ucp.shopping.cart")},
		},
	}

	tests := []struct {
		op   Operation
		want []string
	}{
		{OpCheckout, []string{"dev.ucp.shopping.checkout", "dev.ucp.shopping.discount", "dev.ucp.shopping.fulfillment"}},
		{OpCart, []string{"dev.ucp.shopping.cart", "dev.ucp.shopping.discount"}},
		{OpOrder, []string{"dev.ucp.shopping.order"}},
	}

	for _, tt := range tests {
		t.Run(tt.op.RootCapabilities()[0], func(t *testing.T) {
			got := FilterResponseCapabilities(result, tt.op)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d, want %d (%v vs %v)", len(got), len(tt.want), keys(got), tt.want)
			}
			for _, name := range tt.want {
				if _, ok := got[name]; !ok {
					t.Errorf("missing %q", name)
				}
			}
		})
	}
}

func keys(m map[string]wire.Capability) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
