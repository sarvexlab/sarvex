// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package connector manages store connections, pairing, and the plugin ingest edge.
package connector

import (
	"context"

	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// Service manages connector lifecycle and the plugin ingest edge.
type Service interface {
	AcceptSignal(ctx context.Context, connectorID string, sig domain.ConnectorSignal) error
	RecordHeartbeat(ctx context.Context, connectorID string, hb domain.Heartbeat) error
	FetchProfile(ctx context.Context, connectorID string, etag string) (body []byte, newEtag string, notModified bool, err error)
	GetByConnector(ctx context.Context, connectorID string) (domain.Connector, error)
	GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, domain.Platform, error)
}
