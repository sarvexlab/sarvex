// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	controldomain "github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

type ConnectorService struct {
	repo       ConnectorRepository
	events     eventbus.EventBus
	profiles   ProfileSource
	secretMgmt secretmanagement.SecretManagementService
	log        *slog.Logger
}

func NewConnectorService(
	repo ConnectorRepository,
	events eventbus.EventBus,
	profiles ProfileSource,
	secretMgmt secretmanagement.SecretManagementService,
	log *slog.Logger,
) *ConnectorService {
	return &ConnectorService{
		repo:       repo,
		events:     events,
		profiles:   profiles,
		secretMgmt: secretMgmt,
		log:        log,
	}
}

// DevConnector is a paired connector's identity plus its recovered HMAC
// signing secret. Returned only by DevListConnectors.
type DevConnector struct {
	ConnectorID string
	StoreID     string
	PairingCode string
}

// DevListConnectors returns every paired connector along with its decrypted
// HMAC signing secret (the raw pairing code), recovered the same way
// sync/auth.NewHMACResolver verifies inbound requests. Used only by the
// in-process plugin simulator (internal/planes/sync/sim) in dev mode to
// discover connectors to send heartbeats and signals for; never called in
// production and never exposed over HTTP.
func (s *ConnectorService) DevListConnectors(ctx context.Context) ([]DevConnector, error) {
	models, err := s.repo.ListPaired(ctx)
	if err != nil {
		return nil, fmt.Errorf("list paired connectors: %w", err)
	}

	out := make([]DevConnector, 0, len(models))
	for _, m := range models {
		var encryptedConfig secretmanagement.EncryptedConfig
		if err := json.Unmarshal(m.HMACSecret, &encryptedConfig); err != nil {
			s.log.Warn("dev sim: invalid encrypted hmac secret, skipping", "connector_id", m.ID, "error", err)
			continue
		}

		decrypted, err := s.secretMgmt.Decrypt(ctx, &encryptedConfig)
		if err != nil {
			s.log.Warn("dev sim: hmac secret decryption failed, skipping", "connector_id", m.ID, "error", err)
			continue
		}

		pairingCode, ok := decrypted["hmac_secret"].(string)
		if !ok {
			s.log.Warn("dev sim: hmac_secret missing or not a string, skipping", "connector_id", m.ID)
			continue
		}

		out = append(out, DevConnector{
			ConnectorID: m.ID,
			StoreID:     m.StoreID,
			PairingCode: pairingCode,
		})
	}

	return out, nil
}

// AcceptSignal records a change signal from the plugin and publishes it downstream.
// Takes connectorID to find store context.
func (s *ConnectorService) AcceptSignal(ctx context.Context, connectorID string, sig controldomain.ConnectorSignal) error {
	connector, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("connector not found: %w", err)
	}

	connector.LastSignalAt = ptrTime(time.Now())
	connector.UpdatedAt = time.Now()
	if err := s.repo.Upsert(ctx, connector); err != nil {
		s.log.Error("failed to update connector", "error", err, "connector_id", connectorID)
	}

	entityKind := ""
	switch sig.EntityType {
	case controldomain.EntityProduct:
		entityKind = "product"
	case controldomain.EntityPrice:
		entityKind = "price"
	case controldomain.EntityInventory:
		entityKind = "inventory"
	case controldomain.EntityOrderStatus:
		entityKind = "order_status"
	}

	payload := map[string]any{
		"connector_id": connectorID,
		"store_id":     connector.StoreID,
		"sku":          sig.EntityID,
		"entity_kind":  entityKind,
		"sequence":     sig.Sequence,
		"change":       string(sig.Change),
	}
	payloadBytes, _ := json.Marshal(payload)
	event := newDomainEvent(ucp.EventConnectorSignalReceived, connector.MerchantID, connector.StoreID, payloadBytes)

	if err := s.events.Publish(ctx, event); err != nil {
		s.log.Error("failed to publish signal event", "error", err, "connector_id", connectorID)
		return fmt.Errorf("publish event: %w", err)
	}

	s.log.Info("signal accepted", "connector_id", connectorID, "entity_type", sig.EntityType, "store_id", connector.StoreID)
	return nil
}

// GetStoreCredentials returns the decrypted store API credentials for storeID.
// Read-only: it looks up the connector record the control plane owns and
// writes (pairing, disconnect) and decrypts it, but never writes back. Backs
// the CredentialsProvider passed to base.Registry for the store-API pull
// methods (FetchProduct, FetchCatalog, Ping, ...).
func (s *ConnectorService) GetStoreCredentials(ctx context.Context, storeID string) (map[string]any, controldomain.Platform, error) {
	if s.secretMgmt == nil {
		return nil, "", fmt.Errorf("secret management not configured")
	}

	model, err := s.repo.GetByStoreID(ctx, storeID)
	if err != nil {
		return nil, "", fmt.Errorf("get connector for store %s: %w", storeID, err)
	}

	if model.StoreCredentials == nil {
		return nil, model.Platform, fmt.Errorf("no store credentials for store %s", storeID)
	}

	creds, err := s.secretMgmt.Decrypt(ctx, model.StoreCredentials)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt store credentials for store %s: %w", storeID, err)
	}

	return creds, model.Platform, nil
}

// RecordHeartbeat updates the connector's health status from a plugin heartbeat.
func (s *ConnectorService) RecordHeartbeat(ctx context.Context, connectorID string, hb controldomain.Heartbeat) error {
	connector, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("connector not found: %w", err)
	}

	connector.PluginVersion = ptrString(hb.PluginVersion)
	connector.LastHeartbeatAt = ptrTime(time.Now())
	connector.SiteURL = hb.SiteURL

	if hb.Healthy {
		connector.Status = controldomain.ConnectorStatusHealthy
	} else {
		connector.Status = controldomain.ConnectorStatusDegraded
	}

	connector.UpdatedAt = time.Now()

	if err := s.repo.Upsert(ctx, connector); err != nil {
		return fmt.Errorf("upsert connector: %w", err)
	}

	s.log.Info("heartbeat recorded", "connector_id", connectorID, "healthy", hb.Healthy)
	return nil
}

// GetByConnector retrieves connector by ID (plugin, HMAC auth).
func (s *ConnectorService) GetByConnector(ctx context.Context, connectorID string) (controldomain.Connector, error) {
	model, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return controldomain.Connector{}, err
	}
	return model.ToDomain(), nil
}

// FetchProfile serves the store's UCP profile to the plugin.
// Takes connectorID to find the store context.
func (s *ConnectorService) FetchProfile(ctx context.Context, connectorID string, etag string) (body []byte, newEtag string, notModified bool, err error) {
	connector, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return nil, "", false, fmt.Errorf("connector not found: %w", err)
	}

	body, newEtag, err = s.profiles.GetProfile(ctx, connector.StoreID)
	if err != nil {
		return nil, "", false, fmt.Errorf("get profile: %w", err)
	}
	if body == nil {
		return nil, "", false, ErrProfileNotReady
	}

	if etag != "" && etag == newEtag {
		return nil, newEtag, true, nil
	}

	return body, newEtag, false, nil
}

func newDomainEvent(eventType, merchantID, storeID string, payload []byte) eventbus.DomainEvent {
	return &domainEvent{
		eventType:  eventType,
		merchantID: merchantID,
		storeID:    storeID,
		occurredAt: time.Now(),
		payload:    payload,
	}
}

type domainEvent struct {
	eventType  string
	merchantID string
	storeID    string
	occurredAt time.Time
	payload    []byte
}

func (e *domainEvent) EventType() string     { return e.eventType }
func (e *domainEvent) MerchantID() string    { return e.merchantID }
func (e *domainEvent) StoreID() string       { return e.storeID }
func (e *domainEvent) OccurredAt() time.Time { return e.occurredAt }
func (e *domainEvent) Payload() []byte       { return e.payload }

func ptrString(s string) *string {
	return &s
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

var _ Service = (*ConnectorService)(nil)
