// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package journey

import (
	"context"
	"errors"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// StubService is a Service that returns ErrNotImplemented for every call.
type StubService struct{}

func (StubService) Get(_ context.Context, _ domain.MerchantID, _ domain.StoreID) (domain.Journey, error) {
	return domain.Journey{}, ErrNotImplemented
}

func (StubService) Initialize(_ context.Context, _ domain.MerchantID, _ domain.StoreID) error {
	return ErrNotImplemented
}

func (StubService) HandleSignal(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ Signal) error {
	return ErrNotImplemented
}

var _ Service = StubService{}
