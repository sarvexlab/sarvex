// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/activation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/endpointhealth"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/surface"
)

// The adapters here exist so that activation, surface and endpointhealth
// each keep their own narrow interfaces without importing one another. The
// api package already depends on all three, so composition happens at this
// boundary.

// probeVerifier runs the conformance suite for activation.
//
// It is the endpoint-health probe runner with no feed reader and no
// surface reader, which is not a limitation but the definition: without
// them the runner executes exactly the four store-level probes — profile
// reachable, signature roundtrip, negotiation handshake, catalog lookup —
// against the store's own endpoints, with no external party involved.
// That is what makes `verified` reachable with no surface enrolled.
type probeVerifier struct {
	runner *endpointhealth.ProbeRunner
}

// newProbeVerifier builds the verifier from the same serving base URL and
// probe identity the health worker uses.
func newProbeVerifier(servingBaseURL string, identity *endpointhealth.Identity) *probeVerifier {
	return &probeVerifier{
		runner: endpointhealth.NewProbeRunner(servingBaseURL, identity, endpointhealth.DefaultConfig()),
	}
}

func (v *probeVerifier) Verify(ctx context.Context, storeID domain.StoreID, platform domain.Platform) ([]activation.CheckResult, error) {
	results := v.runner.Run(ctx, "", string(storeID), string(platform))

	checks := make([]activation.CheckResult, 0, len(results))
	for _, r := range results {
		checks = append(checks, activation.CheckResult{
			CheckID: string(r.ProbeType),
			Passed:  r.Outcome == endpointhealth.OutcomePass,
			Detail:  r.Detail,
		})
	}
	return checks, nil
}

var _ activation.Verifier = (*probeVerifier)(nil)

// connectorReaderAdapter narrows connector.Service (which lists by
// merchant) to the per-store list a surface's required-connector rule
// needs.
//
// Filtering here rather than adding a per-store list to connector.Service:
// a merchant has one or two connectors per store in V1, so the filter is
// free, and the surface package gets the single method it actually wants
// instead of a dependency on the whole connector surface.
type connectorReaderAdapter struct {
	connectors connector.Service
}

func (a connectorReaderAdapter) ListByStore(ctx context.Context, merchantID domain.MerchantID, storeID domain.StoreID) ([]domain.Connector, error) {
	if a.connectors == nil {
		return nil, nil
	}
	list, err := a.connectors.List(ctx, merchantID, "", 100)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Connector, 0, len(list.Data))
	for _, c := range list.Data {
		if c.StoreID == storeID {
			out = append(out, c)
		}
	}
	return out, nil
}

var _ surface.ConnectorReader = connectorReaderAdapter{}
