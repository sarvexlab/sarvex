// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package task

import (
	"context"
	"errors"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) List(_ context.Context, _ domain.MerchantID, _ *domain.StoreID, _ string, _ int, _ *domain.TaskOwner, _ *domain.TaskState) (domain.ListOf[domain.Task], error) {
	return domain.ListOf[domain.Task]{}, ErrNotImplemented
}
func (StubService) Get(_ context.Context, _ domain.MerchantID, _ string) (domain.Task, error) {
	return domain.Task{}, ErrNotImplemented
}
func (StubService) Resolve(_ context.Context, _ domain.MerchantID, _ string, _ domain.ResolveTaskRequest) (domain.Task, error) {
	return domain.Task{}, ErrNotImplemented
}
func (StubService) Complete(_ context.Context, _ domain.MerchantID, _ string, _ *string) (domain.Task, error) {
	return domain.Task{}, ErrNotImplemented
}
func (StubService) Reopen(_ context.Context, _ domain.MerchantID, _ string, _ *string) (domain.Task, error) {
	return domain.Task{}, ErrNotImplemented
}
func (StubService) Raise(_ context.Context, _ domain.MerchantID, _ RaiseTaskRequest) (domain.Task, error) {
	return domain.Task{}, ErrNotImplemented
}
func (StubService) ListOpenByMerchants(_ context.Context, _ []domain.MerchantID) (map[domain.StoreID]domain.StoreTaskSummary, error) {
	return nil, ErrNotImplemented
}

var _ Service = StubService{}
