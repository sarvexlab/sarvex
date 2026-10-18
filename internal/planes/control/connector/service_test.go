// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform/magento"
	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	pkgdomain "github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
	"github.com/sarvexlab/sarvex/internal/planes/control/journey"
)

// Mocks for testing

type mockRepository struct {
	getFunc              func(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error)
	getByConnectorFunc   func(ctx context.Context, connectorID string) (*ConnectorModel, error)
	getByStoreIDFunc     func(ctx context.Context, storeID string) (*ConnectorModel, error)
	getByPairingCodeFunc func(ctx context.Context, pairingCode string) (*ConnectorModel, error)
	listFunc             func(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error)
	upsertFunc           func(ctx context.Context, m *ConnectorModel) error
	deleteFunc           func(ctx context.Context, merchantID domain.MerchantID, connectorID string) error
}

func (m *mockRepository) Get(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, merchantID, connectorID)
	}
	return nil, ErrNotFound
}

func (m *mockRepository) GetByConnector(ctx context.Context, connectorID string) (*ConnectorModel, error) {
	if m.getByConnectorFunc != nil {
		return m.getByConnectorFunc(ctx, connectorID)
	}
	return nil, ErrNotFound
}

func (m *mockRepository) GetByStoreID(ctx context.Context, storeID string) (*ConnectorModel, error) {
	if m.getByStoreIDFunc != nil {
		return m.getByStoreIDFunc(ctx, storeID)
	}
	return nil, ErrNotFound
}

func (m *mockRepository) GetByPairingCode(ctx context.Context, pairingCode string) (*ConnectorModel, error) {
	if m.getByPairingCodeFunc != nil {
		return m.getByPairingCodeFunc(ctx, pairingCode)
	}
	return nil, ErrNotFound
}

func (m *mockRepository) List(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, merchantID, afterID, limit)
	}
	return nil, false, nil
}

func (m *mockRepository) Upsert(ctx context.Context, model *ConnectorModel) error {
	if m.upsertFunc != nil {
		return m.upsertFunc(ctx, model)
	}
	return nil
}

func (m *mockRepository) Delete(ctx context.Context, merchantID domain.MerchantID, connectorID string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, merchantID, connectorID)
	}
	return nil
}

type mockEventBus struct {
	published []eventbus.DomainEvent
}

func (m *mockEventBus) Publish(ctx context.Context, event eventbus.DomainEvent) error {
	m.published = append(m.published, event)
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, topic string, handler eventbus.EventHandler) (eventbus.Subscription, error) {
	return nil, errors.New("not implemented")
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, sub eventbus.Subscription) error {
	return errors.New("not implemented")
}

func (m *mockEventBus) Close() error {
	return nil
}

type mockProfileSource struct {
	getProfileFunc func(ctx context.Context, id string) ([]byte, string, error)
}

func (m *mockProfileSource) GetProfile(ctx context.Context, id string) ([]byte, string, error) {
	if m.getProfileFunc != nil {
		return m.getProfileFunc(ctx, id)
	}
	return nil, "", errors.New("profile not found")
}

func (s *mockProfileSource) GetStructuredProfile(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
	return nil, nil
}

// mockSecretManagement stores credentials as plain JSON in the reference
// itself — no real Vault call in tests.
type mockSecretManagement struct{}

func (m *mockSecretManagement) Encrypt(_ context.Context, config map[string]any) (*secretmanagement.EncryptedConfig, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return &secretmanagement.EncryptedConfig{SecretName: "mock", Version: string(data)}, nil
}

func (m *mockSecretManagement) Decrypt(_ context.Context, enc *secretmanagement.EncryptedConfig) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal([]byte(enc.Version), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func newTestService(repo ConnectorRepository, bus eventbus.EventBus, profiles ProfileSource) Service {
	return newTestServiceWithKeys(repo, bus, profiles)
}

func newTestServiceWithKeys(repo ConnectorRepository, bus eventbus.EventBus, profiles ProfileSource) Service {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewConnectorService(repo, bus, profiles, &mockSecretManagement{}, nil, log)
}

func newTestServiceWithCreds(repo ConnectorRepository, bus eventbus.EventBus, profiles ProfileSource, secretMgmt secretmanagement.SecretManagementService, validators CredentialsValidator) Service {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewConnectorService(repo, bus, profiles, secretMgmt, validators, log)
}

// Tests

func TestBeginPairing(t *testing.T) {
	repo := &mockRepository{
		upsertFunc: func(ctx context.Context, m *ConnectorModel) error {
			assert.Equal(t, domain.ConnectorStatusPairing, m.Status)
			assert.NotEmpty(t, m.PairingCodeHash)
			assert.NotNil(t, m.PairingExpiresAt)
			return nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	pairing, err := svc.BeginPairing(context.Background(), domain.MerchantID("test-merchant"), domain.StoreID("test-store"), woocommerce.PlatformName)
	require.NoError(t, err)

	assert.NotEmpty(t, pairing.ConnectorID)
	assert.NotEmpty(t, pairing.PairingCode)
	assert.Len(t, pairing.PairingCode, 32) // 16 bytes hex-encoded
	assert.Equal(t, domain.ConnectorStatusPairing, pairing.Status)
}

// pairingConnector builds a connector model in the pairing state whose hash
// matches pairingCode, with a future expiry, for CompletePairing tests.
func pairingConnector(t *testing.T, connectorID, storeID, pairingCode string) *ConnectorModel {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pairingCode), bcrypt.DefaultCost)
	require.NoError(t, err)
	expires := time.Now().Add(15 * time.Minute)
	return &ConnectorModel{
		ID:               connectorID,
		StoreID:          storeID,
		Platform:         woocommerce.PlatformName,
		Status:           domain.ConnectorStatusPairing,
		PairingCodeHash:  string(hash),
		PairingExpiresAt: &expires,
	}
}

func TestCompletePairing_ProvisionsSigningKeys(t *testing.T) {
	const storeID = "store-1"
	model := pairingConnector(t, "conn-1", storeID, "secret-code")

	var savedHealthy bool
	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, id string) (*ConnectorModel, error) { return model, nil },
		upsertFunc: func(ctx context.Context, m *ConnectorModel) error {
			savedHealthy = m.Status == domain.ConnectorStatusHealthy
			return nil
		},
	}

	svc := newTestServiceWithKeys(repo, &mockEventBus{}, &mockProfileSource{})

	conn, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusHealthy, conn.Status)
	assert.True(t, savedHealthy, "connector should be persisted healthy")
}

func TestCompletePairing_IdempotentWhenKeyExists(t *testing.T) {
	const storeID = "store-1"
	model := pairingConnector(t, "conn-1", storeID, "secret-code")

	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, id string) (*ConnectorModel, error) { return model, nil },
	}

	svc := newTestServiceWithKeys(repo, &mockEventBus{}, &mockProfileSource{})

	_, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", nil)
	require.NoError(t, err)
}

func TestCompletePairing_FailsWhenProvisioningFails(t *testing.T) {
	model := pairingConnector(t, "conn-1", "store-1", "secret-code")

	var upserted bool
	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, id string) (*ConnectorModel, error) { return model, nil },
		upsertFunc: func(ctx context.Context, m *ConnectorModel) error {
			upserted = true
			return nil
		},
	}
	svc := newTestServiceWithKeys(repo, &mockEventBus{}, &mockProfileSource{})

	// Key provisioning was removed from CompletePairing; this now succeeds and marks connector healthy.
	_, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", nil)
	require.NoError(t, err, "CompletePairing should succeed")
	assert.True(t, upserted, "connector should be marked healthy")
}

func TestGet(t *testing.T) {
	expectedModel := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: "merchant-123",
		Platform:   woocommerce.PlatformName,
		Status:     domain.ConnectorStatusHealthy,
	}

	repo := &mockRepository{
		getFunc: func(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error) {
			assert.Equal(t, domain.MerchantID("merchant-123"), merchantID)
			assert.Equal(t, "conn-1", connectorID)
			return expectedModel, nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	connector, err := svc.Get(context.Background(), "merchant-123", "conn-1")
	require.NoError(t, err)
	assert.Equal(t, "conn-1", connector.ConnectorID)
	assert.Equal(t, woocommerce.PlatformName, connector.Platform)
}

func TestGetNotFound(t *testing.T) {
	repo := &mockRepository{
		getFunc: func(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error) {
			return nil, ErrNotFound
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	_, err := svc.Get(context.Background(), "merchant-123", "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestList(t *testing.T) {
	models := []*ConnectorModel{
		{ID: "conn-1", MerchantID: "merchant-123", Platform: woocommerce.PlatformName},
		{ID: "conn-2", MerchantID: "merchant-123", Platform: magento.PlatformName},
	}

	repo := &mockRepository{
		listFunc: func(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error) {
			return models, false, nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	result, err := svc.List(context.Background(), "merchant-123", "", 10)
	require.NoError(t, err)

	assert.Len(t, result.Data, 2)
	assert.False(t, result.Page.HasMore)
	assert.Nil(t, result.Page.Cursor) // No more pages
}

func TestDisconnect(t *testing.T) {
	deleteCalled := false

	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return &ConnectorModel{
				ID:         connectorID,
				MerchantID: "merchant-123",
				StoreID:    "store-123",
				Platform:   woocommerce.PlatformName,
			}, nil
		},
		deleteFunc: func(ctx context.Context, merchantID domain.MerchantID, connectorID string) error {
			deleteCalled = true
			assert.Equal(t, domain.MerchantID("merchant-123"), merchantID)
			assert.Equal(t, "conn-1", connectorID)
			return nil
		},
	}

	bus := &mockEventBus{}
	svc := newTestService(repo, bus, &mockProfileSource{})

	err := svc.Disconnect(context.Background(), "conn-1")
	require.NoError(t, err)
	assert.True(t, deleteCalled)

	// Regression guard: a disconnect used to leave the journey (and thus
	// the console's Path view) parked wherever it was, with no signal
	// telling an open session to route back to /connect — see
	// journey_service.go's SignalConnectorDisconnected transition table.
	require.Len(t, bus.published, 1)
	assert.Equal(t, ucp.EventJourneySignal, bus.published[0].EventType())
	assert.Equal(t, "merchant-123", bus.published[0].MerchantID())
	assert.Equal(t, "store-123", bus.published[0].StoreID())

	var payload journey.JourneySignalPayload
	require.NoError(t, json.Unmarshal(bus.published[0].Payload(), &payload))
	assert.Equal(t, string(journey.SignalConnectorDisconnected), payload.Signal)
}

func TestDisconnectNotFound(t *testing.T) {
	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return nil, ErrNotFound
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	err := svc.Disconnect(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTriggerResync(t *testing.T) {
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: "merchant-123",
		StoreID:    "store-1",
		Platform:   woocommerce.PlatformName,
	}

	repo := &mockRepository{
		getFunc: func(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error) {
			return model, nil
		},
	}

	bus := &mockEventBus{}
	svc := newTestService(repo, bus, &mockProfileSource{})

	job, err := svc.TriggerResync(context.Background(), "merchant-123", "conn-1")
	require.NoError(t, err)

	assert.NotEmpty(t, job.JobID)
	assert.Equal(t, domain.JobQueued, job.Status)

	// Verify a real control.catalog.import command was published — this is
	// what CatalogImportHandler.HandleCommand (sync plane) actually
	// subscribes to and runs; the resync button must drive that, not an
	// unconsumed topic.
	require.Len(t, bus.published, 1)
	assert.Equal(t, "control.catalog.import", bus.published[0].EventType())
	assert.Equal(t, "merchant-123", bus.published[0].MerchantID())
	assert.Equal(t, "store-1", bus.published[0].StoreID())

	var payload catalogImportPayload
	require.NoError(t, json.Unmarshal(bus.published[0].Payload(), &payload))
	assert.Equal(t, "store-1", payload.StoreID)
	assert.Equal(t, "resync", payload.Reason)
	assert.Equal(t, job.JobID, payload.IdempotencyKey)
}

// ── Store credential tests ──────────────────────────────────────────────────

func validWooCreds() map[string]any {
	return map[string]any{
		"store_base_url":  "https://mystore.example.com",
		"consumer_key":    "ck_abc123def456ghi789",
		"consumer_secret": "cs_xyz987uvw654rst321",
		"webhook_secret":  "whs_secret123456789",
		"store_currency":  "USD",
	}
}

func TestCompletePairing_ValidCredsEncryptedAndPersisted(t *testing.T) {
	const storeID = "store-1"
	model := pairingConnector(t, "conn-1", storeID, "secret-code")
	model.Platform = woocommerce.PlatformName

	var savedModel *ConnectorModel
	repo := &mockRepository{
		getByConnectorFunc: func(_ context.Context, _ string) (*ConnectorModel, error) { return model, nil },
		upsertFunc: func(_ context.Context, m *ConnectorModel) error {
			saved := *m
			savedModel = &saved
			return nil
		},
	}

	acceptAllRegistry := &mockValidatorRegistry{}
	svc := newTestServiceWithCreds(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, acceptAllRegistry)

	conn, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", validWooCreds())
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusHealthy, conn.Status)
	require.NotNil(t, savedModel)
	assert.NotNil(t, savedModel.StoreCredentials, "encrypted creds must be persisted on the model")
	assert.Equal(t, "mock", savedModel.StoreCredentials.SecretName)
}

func TestCompletePairing_InvalidCredsRejected(t *testing.T) {
	model := pairingConnector(t, "conn-1", "store-1", "secret-code")
	model.Platform = woocommerce.PlatformName

	var upserted bool
	repo := &mockRepository{
		getByConnectorFunc: func(_ context.Context, _ string) (*ConnectorModel, error) { return model, nil },
		upsertFunc:         func(_ context.Context, _ *ConnectorModel) error { upserted = true; return nil },
	}

	rejectAllRegistry := &mockValidatorRegistry{err: errors.New("missing consumer_key")}
	svc := newTestServiceWithCreds(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, rejectAllRegistry)

	_, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", map[string]any{"bad": "creds"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential validation")
	assert.False(t, upserted, "connector must not be persisted when creds are invalid")
}

func TestCompletePairing_NilCredsSkipsValidation(t *testing.T) {
	model := pairingConnector(t, "conn-1", "store-1", "secret-code")

	repo := &mockRepository{
		getByConnectorFunc: func(_ context.Context, _ string) (*ConnectorModel, error) { return model, nil },
	}

	// Registry would error if called — nil creds must skip it entirely.
	rejectAllRegistry := &mockValidatorRegistry{err: errors.New("should not be called")}
	svc := newTestServiceWithCreds(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, rejectAllRegistry)

	conn, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ConnectorStatusHealthy, conn.Status)
}

// TestCompletePairing_TriggersInitialCatalogImport verifies that a pairing
// completed with store credentials queues the initial catalog import —
// CatalogImportHandler.HandleCommand (sync plane) is what actually pulls the
// catalog once this command lands; without this publish nothing after
// pairing ever ran the "fat pull" for a fresh store.
func TestCompletePairing_TriggersInitialCatalogImport(t *testing.T) {
	model := pairingConnector(t, "conn-1", "store-1", "secret-code")
	model.MerchantID = "merchant-123"
	model.Platform = woocommerce.PlatformName

	repo := &mockRepository{
		getByConnectorFunc: func(_ context.Context, _ string) (*ConnectorModel, error) { return model, nil },
	}

	bus := &mockEventBus{}
	acceptAllRegistry := &mockValidatorRegistry{}
	svc := newTestServiceWithCreds(repo, bus, &mockProfileSource{}, &mockSecretManagement{}, acceptAllRegistry)

	_, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", validWooCreds())
	require.NoError(t, err)

	var imports []eventbus.DomainEvent
	for _, e := range bus.published {
		if e.EventType() == ucp.EventCatalogImport {
			imports = append(imports, e)
		}
	}
	require.Len(t, imports, 1, "expected exactly one control.catalog.import event")
	assert.Equal(t, "merchant-123", imports[0].MerchantID())
	assert.Equal(t, "store-1", imports[0].StoreID())

	var payload catalogImportPayload
	require.NoError(t, json.Unmarshal(imports[0].Payload(), &payload))
	assert.Equal(t, "store-1", payload.StoreID)
	assert.Equal(t, "initial", payload.Reason)
	assert.NotEmpty(t, payload.IdempotencyKey)
}

// TestCompletePairing_NilCredsSkipsCatalogImport verifies a profile-only
// pairing (no store credentials sent) does not queue an import — there are
// no credentials yet for FetchCatalog to pull with.
func TestCompletePairing_NilCredsSkipsCatalogImport(t *testing.T) {
	model := pairingConnector(t, "conn-1", "store-1", "secret-code")
	model.MerchantID = "merchant-123"

	repo := &mockRepository{
		getByConnectorFunc: func(_ context.Context, _ string) (*ConnectorModel, error) { return model, nil },
	}

	bus := &mockEventBus{}
	svc := newTestServiceWithKeys(repo, bus, &mockProfileSource{})

	_, err := svc.CompletePairing(context.Background(), "conn-1", "secret-code", nil)
	require.NoError(t, err)

	for _, e := range bus.published {
		assert.NotEqual(t, ucp.EventCatalogImport, e.EventType(), "profile-only pairing must not queue a catalog import")
	}
}

func TestGetStoreCredentials_RoundTrip(t *testing.T) {
	const storeID = "store-1"
	creds := validWooCreds()
	secretMgmt := &mockSecretManagement{}
	encrypted, err := secretMgmt.Encrypt(context.Background(), creds)
	require.NoError(t, err)

	repo := &mockRepository{
		getByStoreIDFunc: func(_ context.Context, sid string) (*ConnectorModel, error) {
			assert.Equal(t, storeID, sid)
			return &ConnectorModel{
				StoreID:          storeID,
				Platform:         woocommerce.PlatformName,
				StoreCredentials: encrypted,
			}, nil
		},
	}

	svc := newTestServiceWithCreds(repo, &mockEventBus{}, &mockProfileSource{}, secretMgmt, nil)

	got, platform, err := svc.GetStoreCredentials(context.Background(), storeID)
	require.NoError(t, err)
	assert.Equal(t, woocommerce.PlatformName, platform)
	assert.Equal(t, "https://mystore.example.com", got["store_base_url"])
}

func TestGetStoreCredentials_NoCreds(t *testing.T) {
	repo := &mockRepository{
		getByStoreIDFunc: func(_ context.Context, _ string) (*ConnectorModel, error) {
			return &ConnectorModel{StoreID: "store-1", Platform: woocommerce.PlatformName}, nil
		},
	}
	svc := newTestServiceWithCreds(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, nil)

	_, _, err := svc.GetStoreCredentials(context.Background(), "store-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no store credentials")
}

func TestRecordFullSync_OK(t *testing.T) {
	var saved *ConnectorModel
	repo := &mockRepository{
		getByStoreIDFunc: func(_ context.Context, storeID string) (*ConnectorModel, error) {
			return &ConnectorModel{ID: "conn-1", StoreID: storeID, MerchantID: "merch-1"}, nil
		},
		upsertFunc: func(_ context.Context, m *ConnectorModel) error {
			saved = m
			return nil
		},
	}
	svc := NewConnectorService(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, nil, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})))

	at := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	err := svc.RecordFullSync(context.Background(), domain.StoreID("store-1"), at)
	require.NoError(t, err)

	require.NotNil(t, saved)
	require.NotNil(t, saved.LastFullSyncAt)
	assert.Equal(t, at, *saved.LastFullSyncAt)
}

func TestRecordFullSync_StoreNotFound(t *testing.T) {
	repo := &mockRepository{
		getByStoreIDFunc: func(_ context.Context, _ string) (*ConnectorModel, error) {
			return nil, ErrNotFound
		},
	}
	svc := NewConnectorService(repo, &mockEventBus{}, &mockProfileSource{}, &mockSecretManagement{}, nil, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})))

	err := svc.RecordFullSync(context.Background(), domain.StoreID("store-missing"), time.Now())
	assert.Error(t, err)
}

// mockValidatorRegistry implements CredentialsValidator for tests.
type mockValidatorRegistry struct {
	err error
}

func (m *mockValidatorRegistry) ValidateCredentials(_ context.Context, _ pkgdomain.CommerceSystemType, _ string, _ map[string]any) error {
	return m.err
}

// The dev pairing hook learns which platform is being paired, so the plugin
// simulator can answer with that platform's credentials.
func TestBeginPairing_DevHookReceivesThePlatform(t *testing.T) {
	svc := newTestService(&mockRepository{upsertFunc: func(context.Context, *ConnectorModel) error { return nil }}, &mockEventBus{}, &mockProfileSource{})
	var got domain.Platform
	cs, ok := svc.(*ConnectorService)
	require.True(t, ok)
	cs.SetDevPairingHook(func(_, _, _, _ string, p domain.Platform) { got = p })

	_, err := svc.BeginPairing(context.Background(), domain.MerchantID("m"), domain.StoreID("s"), magento.PlatformName)
	require.NoError(t, err)
	assert.Equal(t, magento.PlatformName, got)
}
