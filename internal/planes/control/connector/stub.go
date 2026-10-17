// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

var ErrNotImplemented = errors.New("not implemented")

type StubService struct{}

// Console endpoints (bearer auth)
func (StubService) List(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Connector], error) {
	return domain.ListOf[domain.Connector]{}, ErrNotImplemented
}
func (StubService) Get(_ context.Context, _ domain.MerchantID, _ string) (domain.Connector, error) {
	return domain.Connector{}, ErrNotImplemented
}
func (StubService) TriggerResync(_ context.Context, _ domain.MerchantID, _ string) (domain.JobAccepted, error) {
	return domain.JobAccepted{}, ErrNotImplemented
}

// Pairing flow
func (StubService) BeginPairing(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ domain.Platform) (domain.ConnectorPairing, error) {
	return domain.ConnectorPairing{}, ErrNotImplemented
}
func (StubService) CompletePairing(_ context.Context, _ string, _ string, _ map[string]any) (domain.Connector, error) {
	return domain.Connector{}, ErrNotImplemented
}
func (StubService) GetStoreCredentials(_ context.Context, _ string) (map[string]any, domain.Platform, error) {
	return nil, "", ErrNotImplemented
}

// Plugin endpoints (HMAC auth)
func (StubService) GetByConnector(_ context.Context, _ string) (domain.Connector, error) {
	return domain.Connector{}, ErrNotImplemented
}
func (StubService) Disconnect(_ context.Context, _ string) error {
	return ErrNotImplemented
}
func (StubService) AcceptSignal(_ context.Context, _ string, _ domain.ConnectorSignal) error {
	return ErrNotImplemented
}
func (StubService) RecordHeartbeat(_ context.Context, _ string, _ domain.Heartbeat) error {
	return ErrNotImplemented
}
func (StubService) FetchProfile(_ context.Context, _ string, _ string) ([]byte, string, bool, error) {
	return nil, "", false, ErrNotImplemented
}

var _ Service = StubService{}
