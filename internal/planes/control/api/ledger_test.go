// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/planes/control/account"
	"github.com/sarvexlab/sarvex/internal/planes/control/api"
	"github.com/sarvexlab/sarvex/internal/planes/control/billing"
	"github.com/sarvexlab/sarvex/internal/planes/control/catalog"
	"github.com/sarvexlab/sarvex/internal/planes/control/connector"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
	"github.com/sarvexlab/sarvex/internal/planes/control/ledger"
	"github.com/sarvexlab/sarvex/internal/planes/control/operator"
	"github.com/sarvexlab/sarvex/internal/planes/control/task"
	"github.com/sarvexlab/sarvex/internal/planes/control/validation"
)

// A6/A7: control-plane read API tests for the Lost Revenue Ledger —
// tenancy isolation, pagination terminal page, forward-only status
// transitions, the analytics-tier entitlement gate, and default sort.
//
// fakeLedgerStore is a simplified in-memory Store — like
// fakeCheckoutSessionRepo, it exists to test this package's handler logic,
// not to re-verify Store's real Mongo transactional/index behavior (see
// internal/planes/control/ledger/mongo_store_integration_test.go, tag
// integration, for that).
type fakeLedgerStore struct {
	entries map[string]ledger.Entry
	rollups map[string]ledger.Rollup // "merchantID:month__currency" -> rollup, mirroring mongoStore's rollupID scheme
}

func (f *fakeLedgerStore) InsertEntryAndRollup(context.Context, ledger.Entry) (bool, error) {
	return false, assertNotUsed("InsertEntryAndRollup")
}

func (f *fakeLedgerStore) GetEntry(_ context.Context, merchantID, entryID string) (*ledger.Entry, error) {
	e, ok := f.entries[entryID]
	if !ok || e.MerchantID != merchantID {
		return nil, ledger.ErrNotFound
	}
	cp := e
	return &cp, nil
}

func (f *fakeLedgerStore) ListEntries(_ context.Context, merchantID string, filter ledger.ListFilter, sortMode, cursor string, limit int) ([]ledger.Entry, string, bool, error) {
	var matched []ledger.Entry
	for _, e := range f.entries {
		if e.MerchantID != merchantID {
			continue
		}
		if filter.Cause != "" && e.Cause != filter.Cause {
			continue
		}
		if filter.Status != "" && e.Status != filter.Status {
			continue
		}
		matched = append(matched, e)
	}
	if sortMode == ledger.SortOccurredAtDesc {
		sort.Slice(matched, func(i, j int) bool { return matched[i].OccurredAt.After(matched[j].OccurredAt) })
	} else {
		sort.Slice(matched, func(i, j int) bool {
			if matched[i].LostRevenueMinor != matched[j].LostRevenueMinor {
				return matched[i].LostRevenueMinor > matched[j].LostRevenueMinor
			}
			return matched[i].OccurredAt.After(matched[j].OccurredAt)
		})
	}

	start := 0
	if cursor != "" {
		idx, err := strconv.Atoi(cursor)
		if err != nil || idx < 0 || idx > len(matched) {
			return nil, "", false, ledger.ErrInvalidCursor
		}
		start = idx
	}
	end := start + limit
	hasMore := end < len(matched)
	if end > len(matched) {
		end = len(matched)
	}
	page := matched[start:end]
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(end)
	}
	return page, nextCursor, hasMore, nil
}

func (f *fakeLedgerStore) UpdateEntryStatus(_ context.Context, merchantID, entryID string, next ledger.Status, at time.Time) (*ledger.Entry, error) {
	e, ok := f.entries[entryID]
	if !ok || e.MerchantID != merchantID {
		return nil, ledger.ErrNotFound
	}
	if !ledger.CanTransition(e.Status, next) {
		return nil, ledger.ErrInvalidTransition
	}
	e.Status = next
	e.StatusChangedAt = at
	f.entries[entryID] = e
	cp := e
	return &cp, nil
}

func (f *fakeLedgerStore) GetRollup(_ context.Context, merchantID, month, currency string) (*ledger.Rollup, error) {
	r, ok := f.rollups[merchantID+":"+month+"__"+currency]
	if !ok {
		return nil, ledger.ErrNotFound
	}
	cp := r
	return &cp, nil
}

func (f *fakeLedgerStore) ListRollups(_ context.Context, merchantID, month string) ([]ledger.Rollup, error) {
	var out []ledger.Rollup
	for _, r := range f.rollups {
		if r.MerchantID == merchantID && r.Month == month {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalLostRevenueMinor > out[j].TotalLostRevenueMinor })
	return out, nil
}

func assertNotUsed(name string) error {
	panic("fakeLedgerStore." + name + " not used by these tests")
}

// tieredBillingStub reports an active subscription on a configurable plan
// tier, so tests can exercise requireAnalyticsTier's gate in both
// directions without depending on activeBillingStub's fixed (zero-value
// Plan) shape.
type tieredBillingStub struct {
	billing.StubService
	plan domain.Plan
}

func (t tieredBillingStub) Get(context.Context, domain.MerchantID) (domain.Billing, error) {
	return domain.Billing{Status: domain.BillingActive, Plan: t.plan}, nil
}

func newLedgerTestHandler(store *fakeLedgerStore, plan domain.Plan) (api.Handler, *echo.Echo) {
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
		Ledger: store,
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return h, e
}

func ledgerGet(e *echo.Echo, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func ledgerPatch(e *echo.Echo, path, token string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestLedgerEntries_TenancyIsolation is A7: an entry belonging to another
// merchant must 404 on both GET-adjacent paths (via PATCH, the only
// single-entry route this API exposes) — never a distinct "forbidden".
func TestLedgerEntries_TenancyIsolation(t *testing.T) {
	store := &fakeLedgerStore{entries: map[string]ledger.Entry{
		"led-other": {EntryID: "led-other", MerchantID: "merch-2", Status: ledger.StatusOpen},
	}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerPatch(e, "/v1/ledger/entries/led-other", merchantToken, map[string]string{"status": "acknowledged"}) // resolves to merch-1
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

// TestLedgerEntries_Pagination is A7: cursor round-trips and the terminal
// page reports has_more=false with no cursor.
func TestLedgerEntries_Pagination(t *testing.T) {
	store := &fakeLedgerStore{entries: map[string]ledger.Entry{}}
	base := time.Now().UTC()
	for i := range 5 {
		id := "led-" + strconv.Itoa(i)
		store.entries[id] = ledger.Entry{
			EntryID: id, MerchantID: "merch-1", Status: ledger.StatusOpen,
			LostRevenueMinor: int64(1000 * (5 - i)), OccurredAt: base.Add(-time.Duration(i) * time.Minute),
		}
	}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	seen := map[string]bool{}
	path := "/v1/ledger/entries?limit=2"
	var lastPageHasMore bool
	var lastPageCursor *string
	for range 10 { // bounded loop; 5 entries at limit=2 never needs more than 3 pages
		rec := ledgerGet(e, path, merchantToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Data []struct {
				EntryID string `json:"entry_id"`
			} `json:"data"`
			Page struct {
				Cursor  *string `json:"cursor"`
				HasMore bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		for _, d := range body.Data {
			assert.False(t, seen[d.EntryID], "entry %s seen twice across pages", d.EntryID)
			seen[d.EntryID] = true
		}
		lastPageHasMore = body.Page.HasMore
		lastPageCursor = body.Page.Cursor
		if !body.Page.HasMore {
			break
		}
		path = "/v1/ledger/entries?limit=2&cursor=" + *body.Page.Cursor
	}
	assert.Len(t, seen, 5, "must see every entry exactly once across pages")
	assert.False(t, lastPageHasMore, "terminal page must report has_more=false")
	assert.Nil(t, lastPageCursor, "terminal page must not carry a cursor")
}

// TestLedgerEntries_DefaultSortIsRevenueDescending is A6: the product
// invariant — default GET /ledger/entries returns revenue-descending
// without a sort parameter.
func TestLedgerEntries_DefaultSortIsRevenueDescending(t *testing.T) {
	store := &fakeLedgerStore{entries: map[string]ledger.Entry{
		"led-low":  {EntryID: "led-low", MerchantID: "merch-1", LostRevenueMinor: 500, OccurredAt: time.Now()},
		"led-high": {EntryID: "led-high", MerchantID: "merch-1", LostRevenueMinor: 9000, OccurredAt: time.Now().Add(-time.Hour)},
		"led-mid":  {EntryID: "led-mid", MerchantID: "merch-1", LostRevenueMinor: 3000, OccurredAt: time.Now()},
	}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/ledger/entries", merchantToken) // no sort param
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			EntryID          string `json:"entry_id"`
			LostRevenueMinor int64  `json:"lost_revenue_minor"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 3)
	assert.Equal(t, []string{"led-high", "led-mid", "led-low"}, []string{body.Data[0].EntryID, body.Data[1].EntryID, body.Data[2].EntryID})
}

// TestLedgerEntries_StatusTransition_ForwardOnly is A7: open -> resolved
// succeeds (skip-ahead is forward), but resolved -> acknowledged (a
// reversal) is rejected with 409, never silently applied.
func TestLedgerEntries_StatusTransition_ForwardOnly(t *testing.T) {
	store := &fakeLedgerStore{entries: map[string]ledger.Entry{
		"led-1": {EntryID: "led-1", MerchantID: "merch-1", Status: ledger.StatusOpen},
	}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerPatch(e, "/v1/ledger/entries/led-1", merchantToken, map[string]string{"status": "resolved"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, ledger.StatusResolved, store.entries["led-1"].Status)

	rec = ledgerPatch(e, "/v1/ledger/entries/led-1", merchantToken, map[string]string{"status": "acknowledged"})
	assert.Equal(t, http.StatusConflict, rec.Code, "a reversal from resolved back to acknowledged must be rejected")
	assertErrorEnvelope(t, rec)
	assert.Equal(t, ledger.StatusResolved, store.entries["led-1"].Status, "the rejected transition must not have mutated the entry")
}

// TestLedgerAPI_EntitlementGate is R15/A7: reads are gated at the analytics
// tier (concierge/scale); self_serve is forbidden, not merely empty.
func TestLedgerAPI_EntitlementGate(t *testing.T) {
	store := &fakeLedgerStore{entries: map[string]ledger.Entry{}}

	_, selfServeEcho := newLedgerTestHandler(store, domain.PlanSelfServe)
	rec := ledgerGet(selfServeEcho, "/v1/ledger/entries", merchantToken)
	assert.Equal(t, http.StatusForbidden, rec.Code, "self_serve must not see the ledger")
	assertErrorEnvelope(t, rec)

	rec = ledgerGet(selfServeEcho, "/v1/ledger/summary?currency=USD", merchantToken)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	_, concierge := newLedgerTestHandler(store, domain.PlanConcierge)
	rec = ledgerGet(concierge, "/v1/ledger/entries", merchantToken)
	assert.Equal(t, http.StatusOK, rec.Code, "concierge must see the ledger")

	_, scale := newLedgerTestHandler(store, domain.PlanScale)
	rec = ledgerGet(scale, "/v1/ledger/entries", merchantToken)
	assert.Equal(t, http.StatusOK, rec.Code, "scale must see the ledger")
}

// TestLedgerSummary_CurrencyFilter is the single-currency path: given
// currency, the response is a one-element list scoped to it.
func TestLedgerSummary_CurrencyFilter(t *testing.T) {
	store := &fakeLedgerStore{rollups: map[string]ledger.Rollup{
		"merch-1:2026_08__USD": {MerchantID: "merch-1", Month: "2026_08", Currency: "USD", TotalCount: 3, TotalLostRevenueMinor: 9000},
		"merch-1:2026_08__AUD": {MerchantID: "merch-1", Month: "2026_08", Currency: "AUD", TotalCount: 1, TotalLostRevenueMinor: 4000},
	}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/ledger/summary?month=2026-08&currency=USD", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Currency              string `json:"currency"`
			TotalLostRevenueMinor int64  `json:"total_lost_revenue_minor"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1, "a currency filter must return exactly one element, not every currency")
	assert.Equal(t, "USD", body.Data[0].Currency)
	assert.Equal(t, int64(9000), body.Data[0].TotalLostRevenueMinor)
}

// TestLedgerSummary_AllCurrenciesWithoutFilter is the fix for the console's
// reported friction: omitting currency must not 422 or silently pick one —
// it returns every currency the merchant has a rollup for that month, read
// directly from the ledger's own rollups (never derived from store
// configuration), and never summed across currencies (R9) — each element
// is independently the same shape the single-currency filter returns.
func TestLedgerSummary_AllCurrenciesWithoutFilter(t *testing.T) {
	store := &fakeLedgerStore{rollups: map[string]ledger.Rollup{
		"merch-1:2026_08__USD": {MerchantID: "merch-1", Month: "2026_08", Currency: "USD", TotalCount: 3, TotalLostRevenueMinor: 9000},
		"merch-1:2026_08__AUD": {MerchantID: "merch-1", Month: "2026_08", Currency: "AUD", TotalCount: 1, TotalLostRevenueMinor: 4000},
		// A different merchant's rollup for the same month must never leak in.
		"merch-2:2026_08__USD": {MerchantID: "merch-2", Month: "2026_08", Currency: "USD", TotalCount: 5, TotalLostRevenueMinor: 50000},
	}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/ledger/summary?month=2026-08", merchantToken) // resolves to merch-1, no currency param
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			Currency              string `json:"currency"`
			TotalLostRevenueMinor int64  `json:"total_lost_revenue_minor"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 2, "must return one element per currency the merchant actually has data for")

	byCurrency := map[string]int64{}
	for _, d := range body.Data {
		byCurrency[d.Currency] = d.TotalLostRevenueMinor
	}
	assert.Equal(t, int64(9000), byCurrency["USD"])
	assert.Equal(t, int64(4000), byCurrency["AUD"])
	// R14's money-first ordering applies here too: USD (9000) before AUD (4000).
	assert.Equal(t, "USD", body.Data[0].Currency)
}

// TestLedgerSummary_NoDataReturnsEmptyList covers the brand-new-merchant
// case: no rollup for the month at all is an empty list, not an error.
func TestLedgerSummary_NoDataReturnsEmptyList(t *testing.T) {
	store := &fakeLedgerStore{rollups: map[string]ledger.Rollup{}}
	_, e := newLedgerTestHandler(store, domain.PlanConcierge)

	rec := ledgerGet(e, "/v1/ledger/summary?month=2026-08", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct{} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Empty(t, body.Data)
}
