// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) Board(_ context.Context, _ *domain.JourneyState, _ *bool, _ string, _ int) (domain.ListOf[domain.BoardCard], error) {
	return domain.ListOf[domain.BoardCard]{}, ErrNotImplemented
}
func (StubService) CaptureManualStep(_ context.Context, _ string, _ domain.MerchantID, _ domain.ManualStep) (domain.ManualStep, error) {
	return domain.ManualStep{}, ErrNotImplemented
}
func (StubService) RetagManualStep(_ context.Context, _ string, _ string, _ domain.ManualStepDisposition) error {
	return ErrNotImplemented
}
func (StubService) ListManualSteps(_ context.Context, _ *domain.ManualStepDisposition, _ string, _ int) (domain.ListOf[domain.ManualStep], error) {
	return domain.ListOf[domain.ManualStep]{}, ErrNotImplemented
}
func (StubService) ListManualStepsFiltered(_ context.Context, _ *domain.EngagementStage, _ *domain.ManualStepDisposition, _ string, _ int) (domain.ListOf[domain.ManualStep], error) {
	return domain.ListOf[domain.ManualStep]{}, ErrNotImplemented
}
func (StubService) BulkRetagManualSteps(_ context.Context, _ string, _ []string, _ domain.ManualStepDisposition) error {
	return ErrNotImplemented
}
func (StubService) TimelineMetrics(_ context.Context, _ *domain.MerchantID, _ string, _ string) ([]domain.TimelineDatapoint, error) {
	return nil, ErrNotImplemented
}
func (StubService) AuditLog(_ context.Context, _ *domain.MerchantID, _ string, _ int) (domain.ListOf[domain.AuditEntry], error) {
	return domain.ListOf[domain.AuditEntry]{}, ErrNotImplemented
}
func (StubService) GetPresence(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Presence, error) {
	return domain.Presence{}, ErrNotImplemented
}
func (StubService) GetManifest(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.ManifestView, error) {
	return domain.ManifestView{}, ErrNotImplemented
}
func (StubService) RebuildCache(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.JobAccepted, error) {
	return domain.JobAccepted{}, ErrNotImplemented
}
func (StubService) ListOrders(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *domain.OrderStatus) (domain.ListOf[domain.Order], error) {
	return domain.ListOf[domain.Order]{}, ErrNotImplemented
}
func (StubService) GetOrder(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.Order, error) {
	return domain.Order{}, ErrNotImplemented
}
func (StubService) GetHealth(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Health, error) {
	return domain.Health{}, ErrNotImplemented
}
func (StubService) ListConsistencyFindings(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *domain.Severity) (domain.ListOf[domain.ConsistencyFinding], error) {
	return domain.ListOf[domain.ConsistencyFinding]{}, ErrNotImplemented
}
func (StubService) ActOnBehalf(_ context.Context, _ string, _ domain.MerchantID, _ string, _ map[string]interface{}) error {
	return ErrNotImplemented
}

var _ Service = StubService{}
