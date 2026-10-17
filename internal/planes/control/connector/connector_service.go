// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"golang.org/x/crypto/bcrypt"

	pkgdomain "github.com/sarvexlabs/sarvex.cp/internal/pkg/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/secretmanagement"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp"
	controldomain "github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/journey"
)

type ConnectorService struct {
	repo           ConnectorRepository
	events         eventbus.EventBus
	profiles       ProfileSource
	secretMgmt     secretmanagement.SecretManagementService
	validators     CredentialsValidator
	log            *slog.Logger
	devPairingHook func(connectorID, pairingCode, storeID, merchantID string, platform controldomain.Platform)
}

// SetDevPairingHook registers a callback invoked after every successful
// BeginPairing, with the platform being paired. Used only by the in-process
// plugin simulator in dev mode; never set in production.
func (s *ConnectorService) SetDevPairingHook(fn func(connectorID, pairingCode, storeID, merchantID string, platform controldomain.Platform)) {
	s.devPairingHook = fn
}

func NewConnectorService(
	repo ConnectorRepository,
	events eventbus.EventBus,
	profiles ProfileSource,
	secretMgmt secretmanagement.SecretManagementService,
	validators CredentialsValidator,
	log *slog.Logger,
) *ConnectorService {
	return &ConnectorService{
		repo:       repo,
		events:     events,
		profiles:   profiles,
		secretMgmt: secretMgmt,
		validators: validators,
		log:        log,
	}
}

// List returns connectors for a merchant with pagination.
func (s *ConnectorService) List(ctx context.Context, merchantID controldomain.MerchantID, cursor string, limit int) (controldomain.ListOf[controldomain.Connector], error) {
	models, hasMore, err := s.repo.List(ctx, merchantID, cursor, limit)
	if err != nil {
		return controldomain.ListOf[controldomain.Connector]{}, fmt.Errorf("list connectors: %w", err)
	}

	connectors := lo.Map(models, func(m *ConnectorModel, _ int) controldomain.Connector { return m.ToDomain() })

	var nextCursor *string
	if hasMore && len(models) > 0 {
		nextCursor = ptrString(models[len(models)-1].ID)
	}

	return controldomain.ListOf[controldomain.Connector]{
		Data: connectors,
		Page: controldomain.Page{
			Cursor:  nextCursor,
			HasMore: hasMore,
		},
	}, nil
}

// Get retrieves a connector by ID (console, bearer auth).
func (s *ConnectorService) Get(ctx context.Context, merchantID controldomain.MerchantID, connectorID string) (controldomain.Connector, error) {
	model, err := s.repo.Get(ctx, merchantID, connectorID)
	if err != nil {
		return controldomain.Connector{}, err
	}
	return model.ToDomain(), nil
}

// BeginPairing initiates pairing for a store.
// Accepts merchantID and storeID and stores them in the connector.
func (s *ConnectorService) BeginPairing(ctx context.Context, merchantID controldomain.MerchantID, storeID controldomain.StoreID, platform controldomain.Platform) (controldomain.ConnectorPairing, error) {
	connectorID := uuid.New().String()
	pairingCode, err := generatePairingCode()
	if err != nil {
		return controldomain.ConnectorPairing{}, fmt.Errorf("generate pairing code: %w", err)
	}
	pairingCodeHash, err := bcrypt.GenerateFromPassword([]byte(pairingCode), bcrypt.DefaultCost)
	if err != nil {
		return controldomain.ConnectorPairing{}, fmt.Errorf("hash pairing code: %w", err)
	}

	// Encrypt pairing code to use as HMAC secret for future connector requests.
	// Wrap in a config map for the secret management service.
	hmacSecretEncrypted, err := s.secretMgmt.Encrypt(ctx, map[string]any{
		"hmac_secret": pairingCode,
	})
	if err != nil {
		return controldomain.ConnectorPairing{}, fmt.Errorf("encrypt hmac secret: %w", err)
	}

	// Serialize EncryptedConfig to bytes for storage.
	hmacSecretBytes, err := json.Marshal(hmacSecretEncrypted)
	if err != nil {
		return controldomain.ConnectorPairing{}, fmt.Errorf("marshal encrypted hmac secret: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(15 * time.Minute)

	model := &ConnectorModel{
		ID:               connectorID,
		StoreID:          string(storeID),
		MerchantID:       string(merchantID),
		Platform:         platform,
		Status:           controldomain.ConnectorStatusPairing,
		SyncModel:        controldomain.SyncModelRealtime,
		PairingCodeHash:  string(pairingCodeHash),
		PairingExpiresAt: &expiresAt,
		HMACSecret:       hmacSecretBytes,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.Upsert(ctx, model); err != nil {
		return controldomain.ConnectorPairing{}, fmt.Errorf("upsert connector: %w", err)
	}

	s.log.Info("pairing initiated", "connector_id", connectorID, "store_id", storeID, "platform", platform)

	if s.devPairingHook != nil {
		s.devPairingHook(connectorID, pairingCode, string(storeID), string(merchantID), platform)
	}

	return controldomain.ConnectorPairing{
		Connector:   model.ToDomain(),
		PairingCode: pairingCode,
	}, nil
}

// CompletePairing verifies the pairing code, optionally validates and encrypts
// store credentials, provisions signing keys, and marks the connector healthy.
// storeCreds is nil when the plugin omits credentials (profile-only mode).
func (s *ConnectorService) CompletePairing(ctx context.Context, connectorID string, pairingCode string, storeCreds map[string]any) (controldomain.Connector, error) {
	connector, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		s.log.Warn("connector not found for pairing completion", "connector_id", connectorID)
		return controldomain.Connector{}, fmt.Errorf("connector not found")
	}

	if connector.Status != controldomain.ConnectorStatusPairing {
		s.log.Warn("connector not in pairing state", "connector_id", connectorID, "status", connector.Status)
		return controldomain.Connector{}, fmt.Errorf("connector not in pairing state")
	}

	if connector.PairingExpiresAt == nil || time.Now().After(*connector.PairingExpiresAt) {
		s.log.Warn("pairing code expired", "connector_id", connectorID)
		return controldomain.Connector{}, fmt.Errorf("pairing code expired")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(connector.PairingCodeHash), []byte(pairingCode)); err != nil {
		s.log.Warn("pairing code mismatch", "connector_id", connectorID)
		return controldomain.Connector{}, fmt.Errorf("invalid pairing code")
	}

	if storeCreds != nil {
		if err := s.validateAndPersistCreds(ctx, connector, storeCreds); err != nil {
			return controldomain.Connector{}, err
		}
	}

	connector.Status = controldomain.ConnectorStatusHealthy
	connector.PairingCodeHash = ""
	connector.PairingExpiresAt = nil
	connector.UpdatedAt = time.Now()

	if err := s.repo.Upsert(ctx, connector); err != nil {
		s.log.Error("failed to update connector", "error", err, "connector_id", connectorID)
		return controldomain.Connector{}, fmt.Errorf("update connector: %w", err)
	}

	// Emit journey signal
	journeySignal := journey.JourneySignalPayload{
		Signal: string(journey.SignalConnectorCreated),
	}
	journeyBytes, _ := json.Marshal(journeySignal)
	if err := s.events.Publish(ctx, newDomainEvent(ucp.EventJourneySignal, connector.MerchantID, connector.StoreID, journeyBytes)); err != nil {
		s.log.Error("failed to publish journey signal", "error", err, "merchant_id", connector.MerchantID)
	}

	// Trigger the initial catalog import: the same control.catalog.import
	// command TriggerResync issues for a manual resync, which
	// CatalogImportHandler.HandleCommand (sync plane) already subscribes to
	// and runs for real (FetchCatalog against the store-API client, upsert
	// into the catalog store, score, and emit first_sync_complete on
	// completion). Only fire this when storeCreds was actually provisioned
	// this call — a profile-only pairing (storeCreds == nil) has no
	// credentials yet for FetchCatalog to pull with.
	if storeCreds != nil {
		importPayload := catalogImportPayload{
			StoreID:        connector.StoreID,
			Reason:         "initial",
			IdempotencyKey: uuid.New().String(),
		}
		importBytes, err := json.Marshal(importPayload)
		if err != nil {
			s.log.Error("failed to marshal control.catalog.import payload", "error", err, "connector_id", connectorID)
		} else if err := s.events.Publish(ctx, newDomainEvent(ucp.EventCatalogImport, connector.MerchantID, connector.StoreID, importBytes)); err != nil {
			s.log.Error("failed to publish initial catalog import", "error", err, "connector_id", connectorID)
		}
	}

	s.log.Info("pairing completed", "connector_id", connectorID, "store_id", connector.StoreID, "has_creds", storeCreds != nil)

	return connector.ToDomain(), nil
}

// validateAndPersistCreds validates storeCreds via the per-platform validator
// and encrypts them onto the connector model. Called only when storeCreds != nil.
func (s *ConnectorService) validateAndPersistCreds(ctx context.Context, connector *ConnectorModel, storeCreds map[string]any) error {
	if s.validators == nil {
		return fmt.Errorf("validator registry not configured")
	}
	if s.secretMgmt == nil {
		return fmt.Errorf("secret management not configured")
	}

	platform := pkgdomain.CommerceSystemType(connector.Platform)
	if err := s.validators.ValidateCredentials(ctx, platform, connector.StoreID, storeCreds); err != nil {
		s.log.Warn("store credential validation failed", "error", err, "store_id", connector.StoreID)
		return fmt.Errorf("store credential validation: %w", err)
	}

	encrypted, err := s.secretMgmt.Encrypt(ctx, storeCreds)
	if err != nil {
		s.log.Error("failed to encrypt store credentials", "error", err, "store_id", connector.StoreID)
		return fmt.Errorf("encrypt store credentials: %w", err)
	}

	connector.StoreCredentials = encrypted
	s.log.Info("store credentials validated and encrypted", "store_id", connector.StoreID)
	return nil
}

// GetStoreCredentials returns the decrypted store API credentials for storeID.
// Called by the serving and sync planes to populate Configuration().Config so
// the connector factories can make live calls to the merchant's store.
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

// GetByConnector retrieves connector by ID (plugin, HMAC auth).
func (s *ConnectorService) GetByConnector(ctx context.Context, connectorID string) (controldomain.Connector, error) {
	model, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return controldomain.Connector{}, err
	}
	return model.ToDomain(), nil
}

// RecordFullSync sets a store's connector's last_full_sync_at, satisfying
// journey.ConnectorSyncRecorder. Called when SignalFirstSyncComplete fires
// (on every completed catalog import, not just the store's first).
func (s *ConnectorService) RecordFullSync(ctx context.Context, storeID controldomain.StoreID, at time.Time) error {
	connector, err := s.repo.GetByStoreID(ctx, string(storeID))
	if err != nil {
		return fmt.Errorf("get connector for store %s: %w", storeID, err)
	}
	connector.LastFullSyncAt = &at
	connector.UpdatedAt = time.Now()
	if err := s.repo.Upsert(ctx, connector); err != nil {
		return fmt.Errorf("record full sync for store %s: %w", storeID, err)
	}
	return nil
}

// Disconnect removes a connector and regresses the store's journey back to
// Prospect (SignalConnectorDisconnected — journey_service.go's transition
// table): a deliberate disconnect leaves the store with no connector at
// all, so re-pairing has to start over regardless of how far onboarding
// had progressed. The resulting transition fires control.journey.updated,
// which the console's live journey stream picks up so an open session
// redirects to /connect without a manual refresh.
func (s *ConnectorService) Disconnect(ctx context.Context, connectorID string) error {
	connector, err := s.repo.GetByConnector(ctx, connectorID)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, controldomain.MerchantID(connector.MerchantID), connectorID); err != nil {
		return err
	}

	journeySignal := journey.JourneySignalPayload{
		Signal: string(journey.SignalConnectorDisconnected),
	}
	journeyBytes, _ := json.Marshal(journeySignal)
	if err := s.events.Publish(ctx, newDomainEvent(ucp.EventJourneySignal, connector.MerchantID, connector.StoreID, journeyBytes)); err != nil {
		s.log.Error("failed to publish journey signal", "error", err, "merchant_id", connector.MerchantID)
	}

	s.log.Info("connector disconnected", "connector_id", connectorID)
	return nil
}

// catalogImportPayload mirrors the JSON shape of the sync plane's
// CatalogImportCommandPayload (internal/planes/sync/catalog_import.go) —
// the control.catalog.import command's wire contract. It is redeclared here
// rather than importing the sync package's Go type, since control and sync
// are separate planes that must only share a wire contract, not Go types
// (architecture §4.5, "the event surface is exactly what asyncapi defines").
// Used with Reason "initial" (CompletePairing) and "resync" (TriggerResync).
type catalogImportPayload struct {
	StoreID        string `json:"store_id"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

// TriggerResync queues a full catalog resync for the connector by publishing
// control.catalog.import, the command CatalogImportHandler.HandleCommand
// (internal/planes/sync/catalog_import.go) already subscribes to and runs
// for real (FetchCatalog against the store-API client, upsert into the
// catalog store). Previously this published control.connector.resync_requested,
// a topic nothing in the sync plane subscribed to, so a console "resync"
// action had no effect — this wires the existing button to the existing
// handler; it does not add a new stage.
func (s *ConnectorService) TriggerResync(ctx context.Context, merchantID controldomain.MerchantID, connectorID string) (controldomain.JobAccepted, error) {
	conn, err := s.repo.Get(ctx, merchantID, connectorID)
	if err != nil {
		return controldomain.JobAccepted{}, err
	}

	jobID := uuid.New().String()
	payload := catalogImportPayload{
		StoreID:        conn.StoreID,
		Reason:         "resync",
		IdempotencyKey: jobID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return controldomain.JobAccepted{}, fmt.Errorf("marshal control.catalog.import payload: %w", err)
	}
	event := newDomainEvent(ucp.EventCatalogImport, string(merchantID), conn.StoreID, payloadBytes)

	if err := s.events.Publish(ctx, event); err != nil {
		s.log.Error("failed to publish catalog import event", "error", err, "connector_id", connectorID)
		return controldomain.JobAccepted{}, fmt.Errorf("publish event: %w", err)
	}

	s.log.Info("resync queued", "connector_id", connectorID, "job_id", jobID)

	return controldomain.JobAccepted{
		JobID:  jobID,
		Status: controldomain.JobQueued,
	}, nil
}

func generatePairingCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
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

var _ Service = (*ConnectorService)(nil)
