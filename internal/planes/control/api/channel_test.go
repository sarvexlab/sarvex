// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/channel"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// A6: control-plane read API tests for the Channel Dashboard — range cap,
// tenancy isolation, the analytics-tier entitlement gate, delta
// correctness, and a no-raw-scan assertion (R14). fakeChannelStore/
// fakeBaselineReader are simplified in-memory doubles seeded directly with
// pre-computed aggregates — like ledger_test.go's fakeLedgerStore, they
// exist to test this package's handler logic, not to re-verify the real
// fold/transaction behavior (covered by
// internal/planes/control/channel's own consumer/reconciliation tests and
// mongo_store_integration_test.go).
type fakeChannelStore struct {
	daily map[string]channel.DailyAggregate
}

func newFakeChannelStore() *fakeChannelStore {
	return &fakeChannelStore{daily: map[string]channel.DailyAggregate{}}
}

func (f *fakeChannelStore) put(a channel.DailyAggregate) {
	f.daily[channel.DailyAggregateID(a.StoreID, a.Surface, a.Date)] = a
}

func (f *fakeChannelStore) FoldSessionEvent(context.Context, channel.SessionEvent) (bool, error) {
	return false, assertNotUsed("FoldSessionEvent")
}
func (f *fakeChannelStore) RecordDiscoveryRequest(context.Context, string, string, string, channel.Surface, time.Time) error {
	return assertNotUsed("RecordDiscoveryRequest")
}
func (f *fakeChannelStore) GetDailyAggregate(_ context.Context, merchantID, storeID string, surface channel.Surface, date string) (*channel.DailyAggregate, error) {
	a, ok := f.daily[channel.DailyAggregateID(storeID, surface, date)]
	if !ok || a.MerchantID != merchantID {
		return nil, channel.ErrNotFound
	}
	cp := a
	return &cp, nil
}
func (f *fakeChannelStore) ListDailyAggregates(_ context.Context, merchantID, storeID string, surface channel.Surface, from, to string) ([]channel.DailyAggregate, error) {
	var out []channel.DailyAggregate
	for _, a := range f.daily {
		if a.MerchantID != merchantID {
			continue
		}
		if storeID != "" && a.StoreID != storeID {
			continue
		}
		if surface != "" && a.Surface != surface {
			continue
		}
		if a.Date < from || a.Date > to {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}
func (f *fakeChannelStore) GetSession(context.Context, string) (*channel.Session, error) {
	return nil, assertNotUsed("GetSession")
}

type fakeBaselineReader struct {
	days []channel.BaselineDay
}

func (f *fakeBaselineReader) ListRange(context.Context, string, string, string, string) ([]channel.BaselineDay, error) {
	return f.days, nil
}

func newChannelTestHandler(store *fakeChannelStore, baseline *fakeBaselineReader, plan domain.Plan) (api.Handler, *echo.Echo) {
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
		Channel:     store,
		WebBaseline: baseline,
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return h, e
}

// TestChannelSummary_EntitlementGate is R15: self_serve is denied,
// concierge is allowed — reusing the Ledger's exact gate.
func TestChannelSummary_EntitlementGate(t *testing.T) {
	store := newFakeChannelStore()
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanSelfServe)
	rec := ledgerGet(e, "/v1/channel/summary", merchantToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assertErrorEnvelope(t, rec)

	_, e2 := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)
	rec2 := ledgerGet(e2, "/v1/channel/summary", merchantToken)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// TestChannelFunnel_RangeCap is R13: from/to spanning more than 92 days is
// rejected.
func TestChannelFunnel_RangeCap(t *testing.T) {
	store := newFakeChannelStore()
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/funnel?from=2026-01-01&to=2026-12-31", merchantToken)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assertErrorEnvelope(t, rec)

	rec2 := ledgerGet(e, "/v1/channel/funnel?from=2026-01-01&to=2026-03-01", merchantToken)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// TestChannelDaily_TenancyIsolation is A6: a store_id belonging to a
// different merchant must never surface that merchant's data — the
// server resolves merchant_id from the authenticated principal, never
// from the store_id query param, so a cross-tenant store_id yields an
// empty series (there is no single-resource GET on this API to 404 on;
// see channel.go's doc comment on why every read is merchant_id-scoped by
// construction rather than by a per-request ownership check).
func TestChannelDaily_TenancyIsolation(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-other-merchant", MerchantID: "merch-2", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_06", Sessions: 999, RevenueMinor: 500000})
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_06", Sessions: 3, RevenueMinor: 1500})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/daily?from=2026-08-01&to=2026-08-10", merchantToken) // merchantToken -> merch-1
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Sessions     int64 `json:"sessions"`
			RevenueMinor int64 `json:"revenue_minor"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1, "must never see another merchant's aggregate")
	assert.Equal(t, int64(3), body.Data[0].Sessions)
	assert.Equal(t, int64(1500), body.Data[0].RevenueMinor)
}

// TestChannelSummary_PriorPeriodDelta is A6: prior-period deltas are
// computed server-side from stored counts.
func TestChannelSummary_PriorPeriodDelta(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_15", Sessions: 10, CheckoutsCompleted: 4, RevenueMinor: 40000})
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_07_15", Sessions: 6, CheckoutsCompleted: 2, RevenueMinor: 20000})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/summary?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Surface     string `json:"surface"`
			Sessions    int64  `json:"sessions"`
			PriorPeriod struct {
				SessionsDelta           int64 `json:"sessions_delta"`
				RevenueMinorDelta       int64 `json:"revenue_minor_delta"`
				CheckoutsCompletedDelta int64 `json:"checkouts_completed_delta"`
			} `json:"prior_period"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 2) // "all" + google_gemini
	for _, row := range body.Data {
		assert.Equal(t, int64(10), row.Sessions)
		assert.Equal(t, int64(4), row.PriorPeriod.SessionsDelta)
		assert.Equal(t, int64(20000), row.PriorPeriod.RevenueMinorDelta)
		assert.Equal(t, int64(2), row.PriorPeriod.CheckoutsCompletedDelta)
	}
}

// TestChannelComparison_UnavailableWithoutBaseline is A5/R12: no baseline
// data synced yet must produce available=false with a reason, never an
// estimate.
func TestChannelComparison_UnavailableWithoutBaseline(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_06", RevenueMinor: 5000, CheckoutsCompleted: 2})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/comparison?from=2026-08-01&to=2026-08-10", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Revenue struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"revenue"`
		AgenticShareOfTotal struct {
			Available bool `json:"available"`
		} `json:"agentic_share_of_total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.False(t, body.Revenue.Available)
	assert.NotEmpty(t, body.Revenue.Reason)
	assert.False(t, body.AgenticShareOfTotal.Available)
}

// TestChannelComparison_PartialConfidence_IsAvailableWithReason is A5/R11/
// R12: a day short of connector_reported confidence still yields a real,
// available comparison — computed from the actual (incomplete) data, not
// an estimate — labeled confidence=partial with a reason, rather than
// collapsed into the same unavailable state as a range with zero data.
func TestChannelComparison_PartialConfidence_IsAvailableWithReason(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_01", RevenueMinor: 1000, CheckoutsCompleted: 1})
	baseline := &fakeBaselineReader{days: []channel.BaselineDay{
		{StoreID: "store-1", Date: "2026_08_01", Orders: 5, RevenueMinor: 20000, Confidence: channel.BaselinePartial},
	}}
	_, e := newChannelTestHandler(store, baseline, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/comparison?from=2026-08-01&to=2026-08-01", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Revenue struct {
			Available     bool   `json:"available"`
			Confidence    string `json:"confidence"`
			Reason        string `json:"reason"`
			BaselineMinor int64  `json:"baseline_minor"`
		} `json:"revenue"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.True(t, body.Revenue.Available, "a partial-but-real day must still produce an available comparison")
	assert.Equal(t, "partial", body.Revenue.Confidence)
	assert.NotEmpty(t, body.Revenue.Reason, "partial confidence must explain what's incomplete")
	assert.Equal(t, int64(20000), body.Revenue.BaselineMinor, "partial data is real data, not zeroed out")
}

// TestChannelComparison_AllDaysExplicitlyUnavailable_StillUnavailable is
// R12: every synced day recorded confidence=unavailable (a connector that
// exists but genuinely can't report — see webbaseline's audit) must still
// collapse to available=false, never a fabricated zero-orders "partial".
func TestChannelComparison_AllDaysExplicitlyUnavailable_StillUnavailable(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_01", RevenueMinor: 1000, CheckoutsCompleted: 1})
	baseline := &fakeBaselineReader{days: []channel.BaselineDay{
		{StoreID: "store-1", Date: "2026_08_01", Source: "woocommerce", Confidence: channel.BaselineUnavailable},
	}}
	_, e := newChannelTestHandler(store, baseline, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/comparison?from=2026-08-01&to=2026-08-01", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Revenue     struct{ Available bool }    `json:"revenue"`
		WebBaseline struct{ Confidence string } `json:"web_baseline"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.False(t, body.Revenue.Available)
	assert.Equal(t, "unavailable", body.WebBaseline.Confidence)
}

// TestChannelComparison_ShareOfTotalFormula is R12's audited formula:
// agentic orders already land in the connector's own order stream (see
// api/channel.go's doc comment), so the share of total must be
// agentic/baseline (the connector's total, which already includes
// agentic), never agentic/(agentic+baseline) — this test would catch a
// regression to the double-counting formula.
func TestChannelComparison_ShareOfTotalFormula(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_01", RevenueMinor: 25000, CheckoutsCompleted: 5})
	baseline := &fakeBaselineReader{days: []channel.BaselineDay{
		// The connector's reported total already includes the 25000 of
		// agentic revenue above (R12's audit finding) — it is not
		// "web-only" revenue.
		{StoreID: "store-1", Date: "2026_08_01", Orders: 20, RevenueMinor: 100000, Confidence: channel.BaselineConnectorReported},
	}}
	_, e := newChannelTestHandler(store, baseline, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/comparison?from=2026-08-01&to=2026-08-01", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		AgenticShareOfTotal struct {
			Available bool    `json:"available"`
			Share     float64 `json:"share"`
		} `json:"agentic_share_of_total"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.True(t, body.AgenticShareOfTotal.Available)
	assert.InDelta(t, 0.25, body.AgenticShareOfTotal.Share, 0.0001, "share must be agentic/baseline (0.25), not agentic/(agentic+baseline) (0.2)")
}

// TestChannelComparison_NeverComparesSessionCounts is R12: agent sessions
// and web sessions are never comparable — the comparison response must
// carry no session-count field of any kind, not even an unavailable one.
func TestChannelComparison_NeverComparesSessionCounts(t *testing.T) {
	store := newFakeChannelStore()
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)
	rec := ledgerGet(e, "/v1/channel/comparison", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "session", "comparison response must never mention sessions — agent vs web session counts are not comparable (R12)")
}

// TestChannelDaily_CurrencyExposed is a UI-contract gap fix: currency was
// computed internally but never present in any channel_* response,
// forcing a caller to guess it or borrow it from an unrelated endpoint.
// A single day/store/surface row has no ambiguity, so it's exposed
// directly.
func TestChannelDaily_CurrencyExposed(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_06", RevenueMinor: 5000, CheckoutsCompleted: 1, Currency: "USD"})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/daily?from=2026-08-01&to=2026-08-10", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Currency string `json:"currency"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "USD", body.Data[0].Currency)
}

// TestChannelFunnel_CurrencyMixed_NeverSilentlyPicksOne is the same gap
// for a summed range: two stores/surfaces reporting different currencies
// must never be silently collapsed to one — currency_mixed signals the
// ambiguity explicitly rather than a picked-first-seen value pretending
// to be authoritative.
func TestChannelFunnel_CurrencyMixed_NeverSilentlyPicksOne(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_01", RevenueMinor: 1000, CheckoutsCompleted: 1, Currency: "USD"})
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleAIMode, Date: "2026_08_01", RevenueMinor: 900, CheckoutsCompleted: 1, Currency: "EUR"})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/funnel?from=2026-08-01&to=2026-08-01", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Currency      string `json:"currency"`
		CurrencyMixed bool   `json:"currency_mixed"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Empty(t, body.Currency, "a mixed-currency range must never expose a single picked currency")
	assert.True(t, body.CurrencyMixed)
}

// TestChannelSummary_SessionsConfirmedAndPriorPeriodRateDeltas covers two
// gaps together: sessions_confirmed is exposed explicitly (not left for
// the caller to derive from sessions - sessions_inferred), and
// prior_period carries AOV/completion-rate deltas alongside the existing
// sessions/checkouts/revenue ones.
func TestChannelSummary_SessionsConfirmedAndPriorPeriodRateDeltas(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{
		StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_15",
		Sessions: 10, SessionsInferred: 3, CheckoutsInitiated: 10, CheckoutsCompleted: 5, RevenueMinor: 50000,
	})
	store.put(channel.DailyAggregate{
		StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_07_15",
		Sessions: 10, SessionsInferred: 0, CheckoutsInitiated: 10, CheckoutsCompleted: 2, RevenueMinor: 20000,
	})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/summary?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Surface           string  `json:"surface"`
			Sessions          int64   `json:"sessions"`
			SessionsConfirmed int64   `json:"sessions_confirmed"`
			SessionsInferred  int64   `json:"sessions_inferred"`
			AOVMinor          int64   `json:"aov_minor"`
			CompletionRate    float64 `json:"completion_rate"`
			PriorPeriod       struct {
				AOVMinorDelta       int64   `json:"aov_minor_delta"`
				CompletionRateDelta float64 `json:"completion_rate_delta"`
			} `json:"prior_period"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 2) // "all" + google_gemini
	for _, row := range body.Data {
		assert.Equal(t, int64(10), row.Sessions)
		assert.Equal(t, int64(7), row.SessionsConfirmed, "sessions_confirmed must equal sessions - sessions_inferred")
		assert.Equal(t, int64(3), row.SessionsInferred)
		// current: 50000/5=10000 AOV, 5/10=0.5 completion; prior: 20000/2=10000 AOV, 2/10=0.2 completion.
		assert.Equal(t, int64(10000), row.AOVMinor)
		assert.InDelta(t, 0.5, row.CompletionRate, 0.0001)
		assert.Equal(t, int64(0), row.PriorPeriod.AOVMinorDelta, "AOV unchanged (10000 - 10000)")
		assert.InDelta(t, 0.3, row.PriorPeriod.CompletionRateDelta, 0.0001, "completion rate delta must be 0.5 - 0.2")
	}
}

// TestChannelSummary_SurfaceAllWidening documents the fixed contract gap:
// the combined row's surface is literally "all", not a value drawn from
// the closed 5-member channel_surface enum — the OpenAPI schema for this
// field was widened (channel_surface_or_all) to match what the handler
// actually emits.
func TestChannelSummary_SurfaceAllWidening(t *testing.T) {
	store := newFakeChannelStore()
	store.put(channel.DailyAggregate{StoreID: "store-1", MerchantID: "merch-1", Surface: channel.SurfaceGoogleGemini, Date: "2026_08_06", Sessions: 1})
	_, e := newChannelTestHandler(store, &fakeBaselineReader{}, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/channel/summary?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Surface string `json:"surface"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	var surfaces []string
	for _, row := range body.Data {
		surfaces = append(surfaces, row.Surface)
	}
	assert.Contains(t, surfaces, channel.SurfaceAll)
}
