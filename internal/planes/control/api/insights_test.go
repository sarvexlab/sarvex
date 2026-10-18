// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/insights"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// A7: control-plane read API tests for Agent Behavior Insights — tenancy
// isolation, range cap, the analytics-tier entitlement gate, and a
// no-raw-scan assertion (R15). fakeInsightsStore is a simplified
// in-memory Store — like fakeChannelStore/fakeLedgerStore, it exists to
// test this package's handler logic; Fold* methods panic if called
// because a read handler must never write (and, by the same token, must
// never do anything but call the List* pre-computed-aggregate methods —
// R15's "if a request would scan raw events, the design is wrong").
type fakeInsightsStore struct {
	productDemand   []insights.ProductDemandDaily
	negotiation     []insights.NegotiationDaily
	handlerMismatch []insights.PaymentHandlerMismatchDaily
	capabilityGaps  []insights.CapabilityGapMonthly
}

func (f *fakeInsightsStore) FoldCatalogQuery(context.Context, insights.CatalogQuery) error {
	return assertNotUsed("FoldCatalogQuery")
}
func (f *fakeInsightsStore) FoldNegotiationOutcome(context.Context, insights.NegotiationOutcome) error {
	return assertNotUsed("FoldNegotiationOutcome")
}

func (f *fakeInsightsStore) ListProductDemand(_ context.Context, merchantID, storeID, from, to string) ([]insights.ProductDemandDaily, error) {
	var out []insights.ProductDemandDaily
	for _, r := range f.productDemand {
		if r.MerchantID != merchantID {
			continue
		}
		if storeID != "" && r.StoreID != storeID {
			continue
		}
		if r.Date < from || r.Date > to {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeInsightsStore) ListNegotiationDaily(_ context.Context, merchantID, storeID, from, to string) ([]insights.NegotiationDaily, error) {
	var out []insights.NegotiationDaily
	for _, r := range f.negotiation {
		if r.MerchantID != merchantID {
			continue
		}
		if storeID != "" && r.StoreID != storeID {
			continue
		}
		if r.Date < from || r.Date > to {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeInsightsStore) ListPaymentHandlerMismatchDaily(_ context.Context, merchantID, storeID, from, to string) ([]insights.PaymentHandlerMismatchDaily, error) {
	var out []insights.PaymentHandlerMismatchDaily
	for _, r := range f.handlerMismatch {
		if r.MerchantID != merchantID {
			continue
		}
		if storeID != "" && r.StoreID != storeID {
			continue
		}
		if r.Date < from || r.Date > to {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeInsightsStore) ListCapabilityGaps(_ context.Context, merchantID, storeID, month string) ([]insights.CapabilityGapMonthly, error) {
	var out []insights.CapabilityGapMonthly
	for _, r := range f.capabilityGaps {
		if r.MerchantID != merchantID {
			continue
		}
		if storeID != "" && r.StoreID != storeID {
			continue
		}
		if r.Month != month {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func newInsightsTestHandler(store *fakeInsightsStore, plan domain.Plan) (api.Handler, *echo.Echo) {
	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       tieredBillingStub{plan: plan},
		Operator:      operator.StubService{},
		Validation:    validation.StubService{},
		ResolveBearer: testResolver,
		ResolveHMAC: func(c echo.Context, sig string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
		Insights: store,
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return h, e
}

// TestInsightsCapabilityGaps_EntitlementGate is R16: self_serve is
// denied, concierge is allowed — reusing the Ledger's exact gate.
func TestInsightsCapabilityGaps_EntitlementGate(t *testing.T) {
	store := &fakeInsightsStore{}
	_, e := newInsightsTestHandler(store, domain.PlanSelfServe)
	rec := ledgerGet(e, "/v1/insights/capability_gaps", merchantToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assertErrorEnvelope(t, rec)

	_, e2 := newInsightsTestHandler(store, domain.PlanConcierge)
	rec2 := ledgerGet(e2, "/v1/insights/capability_gaps", merchantToken)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// TestInsightsProductDemand_RangeCap is R15: from/to spanning more than
// 92 days is rejected.
func TestInsightsProductDemand_RangeCap(t *testing.T) {
	store := &fakeInsightsStore{}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/insights/product_demand?from=2026-01-01&to=2026-12-31", merchantToken)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)

	rec2 := ledgerGet(e, "/v1/insights/product_demand?from=2026-01-01&to=2026-03-01", merchantToken)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// TestInsightsNegotiation_RangeCap mirrors the product_demand range cap
// for the negotiation endpoint.
func TestInsightsNegotiation_RangeCap(t *testing.T) {
	store := &fakeInsightsStore{}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/insights/negotiation?from=2026-01-01&to=2026-12-31", merchantToken)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// TestInsightsProductDemand_TenancyIsolation is A7: another merchant's
// product demand row must never appear — the server resolves merchant_id
// from the authenticated principal, never from a request parameter (no
// merchant_id/store_id is even accepted as one), so there is no
// single-resource GET to 404 on; a cross-tenant row is simply absent from
// the result, mirroring channel.TestChannelDaily_TenancyIsolation's
// identical reasoning for the same shape of API.
func TestInsightsProductDemand_TenancyIsolation(t *testing.T) {
	store := &fakeInsightsStore{productDemand: []insights.ProductDemandDaily{
		{MerchantID: "merch-2", StoreID: "store-2", Date: "2026_08_06", ProductID: "sku-other", RequestCount: 999, SurfaceBreakdown: map[string]int64{"google_gemini": 999}},
		{MerchantID: "merch-1", StoreID: "store-1", Date: "2026_08_06", ProductID: "sku-mine", RequestCount: 10, SurfaceBreakdown: map[string]int64{"google_gemini": 10}},
	}}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/insights/product_demand?from=2026-08-01&to=2026-08-10", merchantToken) // merchantToken -> merch-1
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			ProductID    string `json:"product_id"`
			RequestCount int64  `json:"request_count"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1, "must never see another merchant's product demand row")
	assert.Equal(t, "sku-mine", body.Data[0].ProductID)
}

// TestInsightsCapabilityGaps_TenancyIsolation mirrors the tenancy check
// for the headline capability-gap endpoint.
func TestInsightsCapabilityGaps_TenancyIsolation(t *testing.T) {
	store := &fakeInsightsStore{capabilityGaps: []insights.CapabilityGapMonthly{
		{MerchantID: "merch-2", StoreID: "store-2", Month: "2026_08", CapabilityID: "dev.ucp.shopping.other", RequestCount: 999},
		{MerchantID: "merch-1", StoreID: "store-1", Month: "2026_08", CapabilityID: "dev.ucp.shopping.loyalty", RequestCount: 340},
	}}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/insights/capability_gaps?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			CapabilityID string `json:"capability_id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "dev.ucp.shopping.loyalty", body.Data[0].CapabilityID)
}

// TestInsightsCapabilityGaps_DefaultSortIsImpactDescending is A4: the
// default gap ordering (no sort parameter exists on this endpoint at all)
// is estimated_impact_minor descending, request_count descending as
// tiebreak — exercised at the HTTP layer, not just against
// insights.ComputeCapabilityGaps directly (see view_test.go for that).
func TestInsightsCapabilityGaps_DefaultSortIsImpactDescending(t *testing.T) {
	store := &fakeInsightsStore{capabilityGaps: []insights.CapabilityGapMonthly{
		{MerchantID: "merch-1", StoreID: "store-1", Month: "2026_08", CapabilityID: "low-impact-high-count", RequestCount: 1000, EstimatedImpactMinor: 100, ImpactConfidence: insights.ImpactMeasured},
		{MerchantID: "merch-1", StoreID: "store-1", Month: "2026_08", CapabilityID: "high-impact-low-count", RequestCount: 10, EstimatedImpactMinor: 500000, ImpactConfidence: insights.ImpactMeasured},
	}}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/insights/capability_gaps?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			CapabilityID string `json:"capability_id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 2)
	assert.Equal(t, "high-impact-low-count", body.Data[0].CapabilityID)
	assert.Equal(t, "low-impact-high-count", body.Data[1].CapabilityID)
}

// TestInsightsNegotiation_ReadsOnlyPrecomputedAggregates is R15's
// no-raw-scan assertion: FoldCatalogQuery/FoldNegotiationOutcome
// (fakeInsightsStore's write path) panic if called — a passing test here
// proves the negotiation read handler only ever calls
// ListNegotiationDaily/ListPaymentHandlerMismatchDaily.
func TestInsightsNegotiation_ReadsOnlyPrecomputedAggregates(t *testing.T) {
	store := &fakeInsightsStore{negotiation: []insights.NegotiationDaily{
		{MerchantID: "merch-1", StoreID: "store-1", Date: "2026_08_06", Attempts: 10, Successes: 8},
	}}
	_, e := newInsightsTestHandler(store, domain.PlanConcierge)
	rec := ledgerGet(e, "/v1/insights/negotiation?from=2026-08-01&to=2026-08-10", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code) // would have panicked already if a Fold* method were reached
	var body struct {
		Attempts int64 `json:"attempts"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, int64(10), body.Attempts)
}
