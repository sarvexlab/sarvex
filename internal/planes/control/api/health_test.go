// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/endpointhealth"
)

// inMemoryStatusRepo/inMemoryAlertRepo are small standalone fakes (not the
// endpointhealth package's own internal test fakes, which are unexported
// and package-scoped) — just enough to drive the api package's HTTP
// composition/routing logic end to end.
type inMemoryStatusRepo struct {
	docs map[string]*endpointhealth.StatusDocument
}

func (r *inMemoryStatusRepo) Get(_ context.Context, merchantID, storeID string) (*endpointhealth.StatusDocument, error) {
	doc := r.docs[storeID]
	if doc != nil && doc.MerchantID != merchantID {
		return nil, nil
	}
	return doc, nil
}
func (r *inMemoryStatusRepo) Upsert(_ context.Context, doc *endpointhealth.StatusDocument) error {
	r.docs[doc.StoreID] = doc
	return nil
}

type inMemoryAlertRepo struct {
	alerts map[string]*endpointhealth.Alert
}

func (r *inMemoryAlertRepo) Insert(_ context.Context, a *endpointhealth.Alert) error {
	r.alerts[a.ID] = a
	return nil
}
func (r *inMemoryAlertRepo) GetOpenByCheck(_ context.Context, storeID string, checkType endpointhealth.CheckType) (*endpointhealth.Alert, error) {
	for _, a := range r.alerts {
		if a.StoreID == storeID && a.CheckType == checkType && a.Status != endpointhealth.AlertResolved {
			return a, nil
		}
	}
	return nil, nil
}
func (r *inMemoryAlertRepo) Get(_ context.Context, merchantID, alertID string) (*endpointhealth.Alert, error) {
	a, ok := r.alerts[alertID]
	if !ok || a.MerchantID != merchantID {
		return nil, endpointhealth.ErrAlertNotFound
	}
	return a, nil
}
func (r *inMemoryAlertRepo) Update(_ context.Context, a *endpointhealth.Alert) error {
	r.alerts[a.ID] = a
	return nil
}
func (r *inMemoryAlertRepo) List(_ context.Context, merchantID string, filter endpointhealth.AlertListFilter, _ string, _ int) ([]*endpointhealth.Alert, bool, error) {
	var out []*endpointhealth.Alert
	for _, a := range r.alerts {
		if a.MerchantID != merchantID {
			continue
		}
		if filter.StoreID != "" && a.StoreID != filter.StoreID {
			continue
		}
		out = append(out, a)
	}
	return out, false, nil
}

func newTestAlertService() (*endpointhealth.AlertService, *inMemoryAlertRepo) {
	repo := &inMemoryAlertRepo{alerts: map[string]*endpointhealth.Alert{}}
	return endpointhealth.NewAlertService(repo, nil, nil), repo
}

func TestGetHealth_ComposesEndpointStatusAndActiveAlerts(t *testing.T) {
	mockSvc := &mockOperatorService{health: domain.Health{Statements: []domain.HealthStatement{{Kind: domain.HealthConsistency, OK: true}}}}

	statusRepo := &inMemoryStatusRepo{docs: map[string]*endpointhealth.StatusDocument{
		"store-1": {
			StoreID: "store-1", MerchantID: "merch-1", Status: endpointhealth.SeverityCritical, EvaluatedAt: time.Now(),
			Checks: map[endpointhealth.CheckType]endpointhealth.CheckState{
				endpointhealth.CheckSignatureRoundtrip: {State: endpointhealth.OutcomeFail},
			},
		},
	}}
	alertSvc, alertRepo := newTestAlertService()
	alertRepo.alerts["alt_1"] = &endpointhealth.Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", CheckType: endpointhealth.CheckSignatureRoundtrip, Severity: endpointhealth.SeverityCritical, Status: endpointhealth.AlertOpen}

	deps := newTestDeps()
	deps.Operator = mockSvc
	deps.EndpointHealthStatus = statusRepo
	deps.EndpointHealthAlerts = alertSvc

	h := newTestHandlerWithDeps(deps)
	rec := do(h.echo, http.MethodGet, "/v1/stores/store-1/health", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.NotNil(t, body["statements"], "pre-existing consistency statements are untouched")

	endpointStatus, ok := body["endpoint_status"].(map[string]any)
	require.True(t, ok, "endpoint_status must be present when EndpointHealthStatus is wired")
	assert.Equal(t, "critical", endpointStatus["status"])

	activeAlerts, ok := body["active_alerts"].([]any)
	require.True(t, ok)
	require.Len(t, activeAlerts, 1)
}

func TestListAlerts_ScopedToMerchant(t *testing.T) {
	alertSvc, repo := newTestAlertService()
	repo.alerts["alt_mine"] = &endpointhealth.Alert{ID: "alt_mine", MerchantID: "merch-1", StoreID: "store-1", Status: endpointhealth.AlertOpen}
	repo.alerts["alt_other"] = &endpointhealth.Alert{ID: "alt_other", MerchantID: "merch-2", StoreID: "store-9", Status: endpointhealth.AlertOpen}

	deps := newTestDeps()
	deps.EndpointHealthAlerts = alertSvc
	h := newTestHandlerWithDeps(deps)

	rec := do(h.echo, http.MethodGet, "/v1/alerts", bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data []struct {
			AlertID string `json:"alert_id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Len(t, body.Data, 1, "only this merchant's own alert is returned")
	assert.Equal(t, "alt_mine", body.Data[0].AlertID)
}

func TestPatchAlert_ForwardOnly_ConflictOnInvalidTransition(t *testing.T) {
	alertSvc, repo := newTestAlertService()
	now := time.Now()
	repo.alerts["alt_1"] = &endpointhealth.Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", Status: endpointhealth.AlertResolved, ResolvedAt: &now}

	deps := newTestDeps()
	deps.EndpointHealthAlerts = alertSvc
	h := newTestHandlerWithDeps(deps)

	rec := doJSON(h.echo, http.MethodPatch, "/v1/alerts/alt_1", map[string]string{"status": "acknowledged"}, bearer(merchantToken))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestPatchAlert_Acknowledge_OK(t *testing.T) {
	alertSvc, repo := newTestAlertService()
	repo.alerts["alt_1"] = &endpointhealth.Alert{ID: "alt_1", MerchantID: "merch-1", StoreID: "store-1", Status: endpointhealth.AlertOpen}

	deps := newTestDeps()
	deps.EndpointHealthAlerts = alertSvc
	h := newTestHandlerWithDeps(deps)

	rec := doJSON(h.echo, http.MethodPatch, "/v1/alerts/alt_1", map[string]string{"status": "acknowledged"}, bearer(merchantToken))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "acknowledged", got.Status)
}

func TestPatchAlert_CrossTenant_NotFound(t *testing.T) {
	alertSvc, repo := newTestAlertService()
	repo.alerts["alt_1"] = &endpointhealth.Alert{ID: "alt_1", MerchantID: "someone-elses-merchant", StoreID: "store-9", Status: endpointhealth.AlertOpen}

	deps := newTestDeps()
	deps.EndpointHealthAlerts = alertSvc
	h := newTestHandlerWithDeps(deps)

	rec := doJSON(h.echo, http.MethodPatch, "/v1/alerts/alt_1", map[string]string{"status": "acknowledged"}, bearer(merchantToken))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
