// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"errors"

	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

var ErrNotImplemented = errors.New("not implemented")

// ErrProfileNotReady is FetchProfile's answer when the store has no servable
// profile yet (no serving snapshot has been built). The plugin must keep its
// last-known-good copy rather than serve anything new.
var ErrProfileNotReady = errors.New("no servable profile yet")

type StubService struct{}

func (StubService) AcceptSignal(_ context.Context, _ string, _ domain.ConnectorSignal) error {
	return ErrNotImplemented
}
func (StubService) RecordHeartbeat(_ context.Context, _ string, _ domain.Heartbeat) error {
	return ErrNotImplemented
}
func (StubService) FetchProfile(_ context.Context, _ string, _ string) ([]byte, string, bool, error) {
	return nil, "", false, ErrNotImplemented
}
func (StubService) GetByConnector(_ context.Context, _ string) (domain.Connector, error) {
	return domain.Connector{}, ErrNotImplemented
}
func (StubService) GetStoreCredentials(_ context.Context, _ string) (map[string]any, domain.Platform, error) {
	return nil, "", ErrNotImplemented
}

var _ Service = StubService{}
