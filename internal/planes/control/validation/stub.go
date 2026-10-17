// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) GetStatus(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.ValidationStatus, error) {
	return domain.ValidationStatus{}, ErrNotImplemented
}
func (StubService) TriggerRun(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.JobAccepted, error) {
	return domain.JobAccepted{}, ErrNotImplemented
}
func (StubService) ListRuns(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string, _ int) (domain.ListOf[domain.ConformanceRecord], error) {
	return domain.ListOf[domain.ConformanceRecord]{}, ErrNotImplemented
}

var _ Service = StubService{}
