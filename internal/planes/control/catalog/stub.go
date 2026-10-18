// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) GetReadiness(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Readiness, error) {
	return domain.Readiness{}, ErrNotImplemented
}
func (StubService) ListProducts(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *int, _ *bool) (domain.ListOf[domain.ProductSummary], error) {
	return domain.ListOf[domain.ProductSummary]{}, ErrNotImplemented
}
func (StubService) GetProduct(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.ProductDetail, error) {
	return domain.ProductDetail{}, ErrNotImplemented
}
func (StubService) TriggerAudit(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.JobAccepted, error) {
	return domain.JobAccepted{}, ErrNotImplemented
}
func (StubService) ListRemediations(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int, _ *string) (domain.ListOf[domain.Remediation], error) {
	return domain.ListOf[domain.Remediation]{}, ErrNotImplemented
}
func (StubService) ApplyRemediations(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ domain.ApplyRemediationsRequest) (domain.ApplyRemediationsResponse, error) {
	return domain.ApplyRemediationsResponse{}, ErrNotImplemented
}
func (StubService) RevertWriteBack(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.RevertResponse, error) {
	return domain.RevertResponse{}, ErrNotImplemented
}
func (StubService) ListRemediationsForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) ([]domain.Remediation, error) {
	return nil, ErrNotImplemented
}
func (StubService) ListIssuesForSKU(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) ([]domain.ProductIssue, error) {
	return nil, ErrNotImplemented
}

var _ Service = StubService{}
