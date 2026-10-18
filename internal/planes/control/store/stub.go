// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) List(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Store], error) {
	return domain.ListOf[domain.Store]{}, ErrNotImplemented
}

func (StubService) Get(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Store, error) {
	return domain.Store{}, ErrNotImplemented
}

func (StubService) GetByID(_ context.Context, _ domain.StoreID) (domain.Store, error) {
	return domain.Store{}, ErrNotImplemented
}

func (StubService) Create(_ context.Context, _ domain.MerchantID, _ string, _ domain.Platform) (domain.Store, error) {
	return domain.Store{}, ErrNotImplemented
}

func (StubService) Update(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) (domain.Store, error) {
	return domain.Store{}, ErrNotImplemented
}

func (StubService) Delete(_ context.Context, _ domain.MerchantID, _ domain.StoreID) error {
	return ErrNotImplemented
}

func (StubService) LinkConnector(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ string) error {
	return ErrNotImplemented
}

// EnsureSigningKeys reports no key. The stub provisions nothing, and a caller
// that needs a real key id must wire the real service.
func (StubService) EnsureSigningKeys(_ context.Context, _ string) (string, error) {
	return "", nil
}

var _ Service = StubService{}
