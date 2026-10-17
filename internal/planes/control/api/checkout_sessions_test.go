// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/api"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/checkoutescalation"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/operator"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/task"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/validation"
)

// A6: control-plane read API tests — tenancy isolation (cross-tenant ->
// 404), pagination, filter combinations, live-window cap.
//
// The fake repository below uses a simplified index-based cursor, not the
// real production keyset encoding (internal/planes/control/checkoutescalation/cursor.go)
// — it exists to test this package's handler logic (does it pass filters
// through, respect limit, round-trip cursor/has_more correctly), not to
// re-verify the real cursor's byte format. That's exercised by
// checkoutescalation's own tests plus make test-integration against real
// Mongo.

type fakeCheckoutSessionRepo struct {
	sessions map[string]checkoutescalation.Session
}

func (f *fakeCheckoutSessionRepo) ApplyEvent(context.Context, checkoutescalation.ApplyEventParams) (int64, error) {
	return 0, errors.New("not used by read API tests")
}
func (f *fakeCheckoutSessionRepo) MarkAbandoned(context.Context, string, time.Time, checkoutescalation.Diagnosis, time.Duration) error {
	return errors.New("not used by read API tests")
}
func (f *fakeCheckoutSessionRepo) SetDiagnosis(context.Context, string, checkoutescalation.Diagnosis) error {
	return errors.New("not used by read API tests")
}
func (f *fakeCheckoutSessionRepo) ListStaleActive(context.Context, time.Time, int) ([]checkoutescalation.Session, error) {
	return nil, errors.New("not used by read API tests")
}
func (f *fakeCheckoutSessionRepo) ListStalledEscalations(context.Context, time.Time, int) ([]checkoutescalation.Session, error) {
	return nil, errors.New("not used by read API tests")
}

func (f *fakeCheckoutSessionRepo) Get(_ context.Context, merchantID, checkoutID string) (*checkoutescalation.Session, error) {
	s, ok := f.sessions[checkoutID]
	if !ok || s.MerchantID != merchantID {
		return nil, checkoutescalation.ErrNotFound
	}
	cp := s
	return &cp, nil
}

func (f *fakeCheckoutSessionRepo) matching(merchantID string, filter checkoutescalation.ListFilter) []checkoutescalation.Session {
	var out []checkoutescalation.Session
	for _, s := range f.sessions {
		if s.MerchantID != merchantID {
			continue
		}
		if filter.State != "" && s.State != filter.State {
			continue
		}
		if filter.Surface != "" && s.Surface != filter.Surface {
			continue
		}
		if filter.StoreID != "" && s.StoreID != filter.StoreID {
			continue
		}
		if filter.Cause != "" && (s.Diagnosis == nil || s.Diagnosis.Cause != filter.Cause) {
			continue
		}
		if filter.EscalatedOnly && s.Escalation == nil {
			continue
		}
		if filter.From != nil && s.LastActivityAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && s.LastActivityAt.After(*filter.To) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivityAt.Equal(out[j].LastActivityAt) {
			return out[i].LastActivityAt.After(out[j].LastActivityAt)
		}
		return out[i].CheckoutID > out[j].CheckoutID
	})
	return out
}

func (f *fakeCheckoutSessionRepo) List(_ context.Context, merchantID string, filter checkoutescalation.ListFilter, cursor string, limit int) ([]checkoutescalation.Session, string, bool, error) {
	matched := f.matching(merchantID, filter)

	start := 0
	if cursor != "" {
		idx, err := strconv.Atoi(cursor)
		if err != nil || idx < 0 || idx > len(matched) {
			return nil, "", false, checkoutescalation.ErrInvalidCursor
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

func (f *fakeCheckoutSessionRepo) ListLive(_ context.Context, merchantID string, window time.Duration, limit int) ([]checkoutescalation.Session, bool, error) {
	cutoff := time.Now().UTC().Add(-window)
	var out []checkoutescalation.Session
	for _, s := range f.sessions {
		if s.MerchantID != merchantID || s.State.Terminal() {
			continue
		}
		if s.LastActivityAt.Before(cutoff) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActivityAt.After(out[j].LastActivityAt) })
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

type fakeCheckoutTimelineRepo struct {
	entries map[string][]checkoutescalation.TimelineEntry
}

func (f *fakeCheckoutTimelineRepo) Insert(context.Context, checkoutescalation.TimelineEntry) (bool, error) {
	return false, errors.New("not used by read API tests")
}
func (f *fakeCheckoutTimelineRepo) SetSeq(context.Context, string, string, int64) error {
	return errors.New("not used by read API tests")
}
func (f *fakeCheckoutTimelineRepo) SetExpiry(context.Context, string, time.Time) error {
	return errors.New("not used by read API tests")
}
func (f *fakeCheckoutTimelineRepo) ListEvidence(context.Context, string, []string) ([]checkoutescalation.TimelineEntry, error) {
	return nil, errors.New("not used by read API tests")
}

func (f *fakeCheckoutTimelineRepo) List(_ context.Context, checkoutID string, afterSeq int64, limit int) ([]checkoutescalation.TimelineEntry, bool, error) {
	all := f.entries[checkoutID]
	var page []checkoutescalation.TimelineEntry
	for _, e := range all {
		if e.Seq > afterSeq {
			page = append(page, e)
		}
	}
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	return page, hasMore, nil
}

func newCheckoutSessionsTestHandler(sessions *fakeCheckoutSessionRepo, timeline *fakeCheckoutTimelineRepo) (api.Handler, *echo.Echo) {
	deps := api.Dependencies{
		Account:       account.StubService{},
		Connector:     connector.StubService{},
		Catalog:       catalog.StubService{},
		Task:          task.StubService{},
		Journey:       journey.StubService{},
		Billing:       activeBillingStub{},
		Operator:      operator.StubService{},
		Validation:    validation.StubService{},
		ResolveBearer: testResolver,
		ResolveHMAC: func(c echo.Context, sig string) (domain.Principal, error) {
			return domain.Principal{MerchantID: "test"}, nil
		},
		CheckoutSessions:        sessions,
		CheckoutSessionTimeline: timeline,
	}
	h := api.New(deps)
	e := echo.New()
	g := e.Group("/v1")
	_ = h.SetupRoutes("/v1", g)
	return h, e
}

func mustGet(t *testing.T, e *echo.Echo, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestCheckoutSessions_TenancyIsolation is A6: a session belonging to
// another merchant must 404, not surface any detail distinguishing "wrong
// tenant" from "doesn't exist" (R11's tenancy invariant).
func TestCheckoutSessions_TenancyIsolation(t *testing.T) {
	sessions := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{
		"checkout-other-merchant": {CheckoutID: "checkout-other-merchant", MerchantID: "merch-2", StoreID: "store-9", State: checkoutescalation.StateCompleted},
	}}
	_, e := newCheckoutSessionsTestHandler(sessions, &fakeCheckoutTimelineRepo{})

	rec := mustGet(t, e, "/v1/checkout-sessions/checkout-other-merchant", merchantToken) // resolves to merch-1
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assertErrorEnvelope(t, rec)
}

func TestCheckoutSessions_TenancyIsolation_Timeline(t *testing.T) {
	sessions := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{
		"checkout-other-merchant": {CheckoutID: "checkout-other-merchant", MerchantID: "merch-2", StoreID: "store-9", State: checkoutescalation.StateCompleted},
	}}
	timeline := &fakeCheckoutTimelineRepo{entries: map[string][]checkoutescalation.TimelineEntry{
		"checkout-other-merchant": {{CheckoutID: "checkout-other-merchant", Seq: 1, EventID: "ev-1", EventType: "serving.checkout.session_initiated"}},
	}}
	_, e := newCheckoutSessionsTestHandler(sessions, timeline)

	rec := mustGet(t, e, "/v1/checkout-sessions/checkout-other-merchant/timeline", merchantToken)
	assert.Equal(t, http.StatusNotFound, rec.Code, "timeline of a session owned by a different merchant must 404, never leak entries")
}

func TestCheckoutSessions_Get_OwnSession(t *testing.T) {
	sessions := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{
		"checkout-1": {
			CheckoutID: "checkout-1", MerchantID: "merch-1", StoreID: "store-1",
			State: checkoutescalation.StateFailed, CurrentStage: "complete",
			Diagnosis: &checkoutescalation.Diagnosis{StageReached: "complete", Cause: "order_creation_failed", Evidence: []int64{3}},
		},
	}}
	_, e := newCheckoutSessionsTestHandler(sessions, &fakeCheckoutTimelineRepo{})

	rec := mustGet(t, e, "/v1/checkout-sessions/checkout-1", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestCheckoutSessions_Pagination is A6: cursor round-trips and has_more
// is honored across pages, with no duplicate or skipped rows.
func TestCheckoutSessions_Pagination(t *testing.T) {
	repo := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{}}
	base := time.Now().UTC()
	for i := range 5 {
		id := "checkout-" + strconv.Itoa(i)
		repo.sessions[id] = checkoutescalation.Session{
			CheckoutID: id, MerchantID: "merch-1", StoreID: "store-1",
			State: checkoutescalation.StateCartActive, LastActivityAt: base.Add(-time.Duration(i) * time.Minute),
		}
	}
	_, e := newCheckoutSessionsTestHandler(repo, &fakeCheckoutTimelineRepo{})

	seen := map[string]bool{}
	path := "/v1/checkout-sessions?limit=2"
	for range 10 { // bounded loop, never actually needs more than 3 pages
		rec := mustGet(t, e, path, merchantToken)
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Data []struct {
				CheckoutID string `json:"checkout_id"`
			} `json:"data"`
			Page struct {
				Cursor  *string `json:"cursor"`
				HasMore bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		for _, s := range body.Data {
			assert.False(t, seen[s.CheckoutID], "checkout_id %s seen twice across pages", s.CheckoutID)
			seen[s.CheckoutID] = true
		}
		if !body.Page.HasMore {
			break
		}
		require.NotNil(t, body.Page.Cursor)
		path = "/v1/checkout-sessions?limit=2&cursor=" + *body.Page.Cursor
	}
	assert.Len(t, seen, 5, "expected all 5 sessions to be seen exactly once across pages")
}

// TestCheckoutSessions_FilterCombinations is A6.
func TestCheckoutSessions_FilterCombinations(t *testing.T) {
	repo := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{
		"c1": {CheckoutID: "c1", MerchantID: "merch-1", StoreID: "store-a", State: checkoutescalation.StateFailed, LastActivityAt: time.Now(), Diagnosis: &checkoutescalation.Diagnosis{Cause: "order_creation_failed"}},
		"c2": {CheckoutID: "c2", MerchantID: "merch-1", StoreID: "store-b", State: checkoutescalation.StateFailed, LastActivityAt: time.Now(), Diagnosis: &checkoutescalation.Diagnosis{Cause: "pricing_stale"}},
		"c3": {CheckoutID: "c3", MerchantID: "merch-1", StoreID: "store-a", State: checkoutescalation.StateEscalated, LastActivityAt: time.Now(), Escalation: &checkoutescalation.Escalation{Reason: "3ds"}},
		"c4": {CheckoutID: "c4", MerchantID: "merch-1", StoreID: "store-a", State: checkoutescalation.StateCompleted, LastActivityAt: time.Now()},
	}}
	_, e := newCheckoutSessionsTestHandler(repo, &fakeCheckoutTimelineRepo{})

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"state filter", "state=failed", []string{"c1", "c2"}},
		{"store_id filter", "store_id=store-a", []string{"c1", "c3", "c4"}},
		{"cause filter", "cause=pricing_stale", []string{"c2"}},
		{"escalated_only", "escalated_only=true", []string{"c3"}},
		{"combined state+store_id", "state=failed&store_id=store-a", []string{"c1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := mustGet(t, e, "/v1/checkout-sessions?"+tt.query, merchantToken)
			require.Equal(t, http.StatusOK, rec.Code)
			var body struct {
				Data []struct {
					CheckoutID string `json:"checkout_id"`
				} `json:"data"`
			}
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			var got []string
			for _, s := range body.Data {
				got = append(got, s.CheckoutID)
			}
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

// TestCheckoutSessions_LiveWindowCap is A6: non-terminal sessions inside
// the window only, capped, with has_more reflecting truncation.
func TestCheckoutSessions_LiveWindowCap(t *testing.T) {
	repo := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{
		"in-window":      {CheckoutID: "in-window", MerchantID: "merch-1", StoreID: "s1", State: checkoutescalation.StateCartActive, LastActivityAt: time.Now().UTC().Add(-5 * time.Minute)},
		"outside-window": {CheckoutID: "outside-window", MerchantID: "merch-1", StoreID: "s1", State: checkoutescalation.StateCartActive, LastActivityAt: time.Now().UTC().Add(-45 * time.Minute)},
		"terminal":       {CheckoutID: "terminal", MerchantID: "merch-1", StoreID: "s1", State: checkoutescalation.StateCompleted, LastActivityAt: time.Now().UTC().Add(-1 * time.Minute)},
	}}
	_, e := newCheckoutSessionsTestHandler(repo, &fakeCheckoutTimelineRepo{})

	rec := mustGet(t, e, "/v1/checkout-sessions/live", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []struct {
			CheckoutID string `json:"checkout_id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "in-window", body.Data[0].CheckoutID)
}

func TestCheckoutSessions_LiveWindowCap_Truncation(t *testing.T) {
	repo := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{}}
	for i := range 5 {
		id := "live-" + strconv.Itoa(i)
		repo.sessions[id] = checkoutescalation.Session{
			CheckoutID: id, MerchantID: "merch-1", StoreID: "s1",
			State: checkoutescalation.StateCartActive, LastActivityAt: time.Now().UTC().Add(-time.Duration(i) * time.Second),
		}
	}
	_, e := newCheckoutSessionsTestHandler(repo, &fakeCheckoutTimelineRepo{})

	rec := mustGet(t, e, "/v1/checkout-sessions/live?limit=2", merchantToken)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []any `json:"data"`
		Page struct {
			HasMore bool `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Len(t, body.Data, 2)
	assert.True(t, body.Page.HasMore, "expected has_more=true when the live cap truncates the result")
}

func TestCheckoutSessions_InvalidCursor_Returns422(t *testing.T) {
	repo := &fakeCheckoutSessionRepo{sessions: map[string]checkoutescalation.Session{}}
	_, e := newCheckoutSessionsTestHandler(repo, &fakeCheckoutTimelineRepo{})

	rec := mustGet(t, e, "/v1/checkout-sessions?cursor=not-a-valid-cursor", merchantToken)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
