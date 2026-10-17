// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/connector"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

// stubConnectorService is a controllable connector.Service for component tests.
type stubConnectorService struct {
	connectors   []domain.Connector
	pairing      domain.ConnectorPairing
	getErr       error
	listErr      error
	pairingErr   error
	completErr   error
	getByConnErr error
	discErr      error
	resyncErr    error
	signalErr    error
	hbErr        error
	profileErr   error
	profile      []byte
	etag         string
	connector    domain.Connector
}

// Console endpoints (bearer auth)
func (s *stubConnectorService) List(_ context.Context, _ domain.MerchantID, _ string, _ int) (domain.ListOf[domain.Connector], error) {
	if s.listErr != nil {
		return domain.ListOf[domain.Connector]{}, s.listErr
	}
	return domain.ListOf[domain.Connector]{
		Data: s.connectors,
		Page: domain.Page{Cursor: nil, HasMore: false},
	}, nil
}

func (s *stubConnectorService) Get(_ context.Context, _ domain.MerchantID, _ string) (domain.Connector, error) {
	if s.getErr != nil {
		return domain.Connector{}, s.getErr
	}
	return s.connector, nil
}

func (s *stubConnectorService) TriggerResync(_ context.Context, _ domain.MerchantID, _ string) (domain.JobAccepted, error) {
	if s.resyncErr != nil {
		return domain.JobAccepted{}, s.resyncErr
	}
	return domain.JobAccepted{JobID: "job-1", Status: domain.JobQueued}, nil
}

// Pairing flow
func (s *stubConnectorService) BeginPairing(_ context.Context, _ domain.MerchantID, _ domain.StoreID, _ domain.Platform) (domain.ConnectorPairing, error) {
	if s.pairingErr != nil {
		return domain.ConnectorPairing{}, s.pairingErr
	}
	return s.pairing, nil
}

func (s *stubConnectorService) CompletePairing(_ context.Context, _ string, _ string, _ map[string]any) (domain.Connector, error) {
	if s.completErr != nil {
		return domain.Connector{}, s.completErr
	}
	return s.connector, nil
}

func (s *stubConnectorService) GetStoreCredentials(_ context.Context, _ string) (map[string]any, domain.Platform, error) {
	return nil, "", connector.ErrNotImplemented
}

// Plugin endpoints (HMAC auth)
func (s *stubConnectorService) GetByConnector(_ context.Context, _ string) (domain.Connector, error) {
	if s.getByConnErr != nil {
		return domain.Connector{}, s.getByConnErr
	}
	return s.connector, nil
}

func (s *stubConnectorService) Disconnect(_ context.Context, _ string) error {
	return s.discErr
}

func (s *stubConnectorService) AcceptSignal(_ context.Context, _ string, _ domain.ConnectorSignal) error {
	return s.signalErr
}

func (s *stubConnectorService) RecordHeartbeat(_ context.Context, _ string, _ domain.Heartbeat) error {
	return s.hbErr
}

func (s *stubConnectorService) FetchProfile(_ context.Context, _ string, _ string) ([]byte, string, bool, error) {
	if s.profileErr != nil {
		return nil, "", false, s.profileErr
	}
	return s.profile, s.etag, false, nil
}

var _ connector.Service = (*stubConnectorService)(nil)
