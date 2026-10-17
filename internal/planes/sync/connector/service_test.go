// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package connector

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/planes/sync/domain"
)

// Mocks for testing

type mockRepository struct {
	getFunc              func(ctx context.Context, merchantID domain.MerchantID, connectorID string) (*ConnectorModel, error)
	getByConnectorFunc   func(ctx context.Context, connectorID string) (*ConnectorModel, error)
	getByStoreIDFunc     func(ctx context.Context, storeID string) (*ConnectorModel, error)
	getByPairingCodeFunc func(ctx context.Context, pairingCode string) (*ConnectorModel, error)
	listFunc             func(ctx context.Context, merchantID domain.MerchantID, afterID string, limit int) ([]*ConnectorModel, bool, error)
	listPairedFunc       func(ctx context.Context) ([]*ConnectorModel, error)
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

func (m *mockRepository) ListPaired(ctx context.Context) ([]*ConnectorModel, error) {
	if m.listPairedFunc != nil {
		return m.listPairedFunc(ctx)
	}
	return nil, nil
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
	return NewConnectorService(repo, bus, profiles, &mockSecretManagement{}, log)
}

func TestRecordHeartbeat(t *testing.T) {
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: "merchant-123",
		Platform:   woocommerce.PlatformName,
	}

	upsertCalled := false
	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return model, nil
		},
		upsertFunc: func(ctx context.Context, m *ConnectorModel) error {
			upsertCalled = true
			assert.Equal(t, "conn-1", m.ID)
			assert.Equal(t, domain.ConnectorStatusHealthy, m.Status)
			assert.Equal(t, "1.0.0", *m.PluginVersion)
			assert.Equal(t, "https://example.com", m.SiteURL)
			return nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	hb := domain.Heartbeat{
		PluginVersion:   "1.0.0",
		PlatformVersion: "5.0.0",
		SiteURL:         "https://example.com",
		Healthy:         true,
	}

	err := svc.RecordHeartbeat(context.Background(), "conn-1", hb)
	require.NoError(t, err)
	assert.True(t, upsertCalled)
}

func TestRecordHeartbeatDegraded(t *testing.T) {
	model := &ConnectorModel{
		ID:         "conn-1",
		MerchantID: "merchant-123",
		Platform:   woocommerce.PlatformName,
		Status:     domain.ConnectorStatusHealthy,
	}

	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return model, nil
		},
		upsertFunc: func(ctx context.Context, m *ConnectorModel) error {
			// When healthy=false, status should be degraded
			assert.Equal(t, domain.ConnectorStatusDegraded, m.Status)
			return nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	hb := domain.Heartbeat{
		PluginVersion: "1.0.0",
		Healthy:       false,
	}

	err := svc.RecordHeartbeat(context.Background(), "conn-1", hb)
	require.NoError(t, err)
}

func TestGetStoreCredentials(t *testing.T) {
	secretMgmt := &mockSecretManagement{}
	encrypted, err := secretMgmt.Encrypt(context.Background(), map[string]any{
		"store_base_url":  "https://store.example.com",
		"consumer_key":    "ck_test",
		"consumer_secret": "cs_test",
	})
	require.NoError(t, err)

	model := &ConnectorModel{
		ID:               "conn-1",
		StoreID:          "store-1",
		MerchantID:       "merchant-123",
		Platform:         woocommerce.PlatformName,
		StoreCredentials: encrypted,
	}

	repo := &mockRepository{
		getByStoreIDFunc: func(ctx context.Context, storeID string) (*ConnectorModel, error) {
			assert.Equal(t, "store-1", storeID)
			return model, nil
		},
	}

	svc := NewConnectorService(repo, &mockEventBus{}, &mockProfileSource{}, secretMgmt,
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})))

	creds, platform, err := svc.GetStoreCredentials(context.Background(), "store-1")
	require.NoError(t, err)
	assert.Equal(t, woocommerce.PlatformName, platform)
	assert.Equal(t, "https://store.example.com", creds["store_base_url"])
	assert.Equal(t, "ck_test", creds["consumer_key"])
}

func TestGetStoreCredentials_NotFound(t *testing.T) {
	repo := &mockRepository{
		getByStoreIDFunc: func(ctx context.Context, storeID string) (*ConnectorModel, error) {
			return nil, ErrNotFound
		},
	}

	svc := newTestService(repo, &mockEventBus{}, &mockProfileSource{})

	_, _, err := svc.GetStoreCredentials(context.Background(), "does-not-exist")
	require.Error(t, err)
}

func TestDevListConnectors(t *testing.T) {
	secretMgmt := &mockSecretManagement{}
	encrypted, err := secretMgmt.Encrypt(context.Background(), map[string]any{"hmac_secret": "raw-pairing-code"})
	require.NoError(t, err)
	hmacSecretBytes, err := json.Marshal(encrypted)
	require.NoError(t, err)

	repo := &mockRepository{
		listPairedFunc: func(ctx context.Context) ([]*ConnectorModel, error) {
			return []*ConnectorModel{
				{ID: "conn-1", StoreID: "store-1", HMACSecret: hmacSecretBytes},
			}, nil
		},
	}

	svc := NewConnectorService(repo, &mockEventBus{}, &mockProfileSource{}, secretMgmt,
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})))

	devConnectors, err := svc.DevListConnectors(context.Background())
	require.NoError(t, err)
	require.Len(t, devConnectors, 1)
	assert.Equal(t, "conn-1", devConnectors[0].ConnectorID)
	assert.Equal(t, "store-1", devConnectors[0].StoreID)
	assert.Equal(t, "raw-pairing-code", devConnectors[0].PairingCode)
}

func TestDevListConnectors_SkipsUndecryptable(t *testing.T) {
	repo := &mockRepository{
		listPairedFunc: func(ctx context.Context) ([]*ConnectorModel, error) {
			return []*ConnectorModel{
				{ID: "conn-bad", StoreID: "store-1", HMACSecret: []byte("not-json")},
			}, nil
		},
	}

	svc := newTestServiceWithKeys(repo, &mockEventBus{}, &mockProfileSource{}).(*ConnectorService)

	devConnectors, err := svc.DevListConnectors(context.Background())
	require.NoError(t, err)
	assert.Empty(t, devConnectors)
}

func TestFetchProfile(t *testing.T) {
	model := &ConnectorModel{
		ID:       "conn-1",
		StoreID:  "store-1",
		Platform: woocommerce.PlatformName,
	}

	profileBody := []byte(`{"ucp": {"version": "1.0"}}`)
	profileETag := "abc123"

	profiles := &mockProfileSource{
		getProfileFunc: func(ctx context.Context, id string) ([]byte, string, error) {
			return profileBody, profileETag, nil
		},
	}

	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return model, nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, profiles)

	body, etag, notModified, err := svc.FetchProfile(context.Background(), "conn-1", "")
	require.NoError(t, err)
	assert.Equal(t, profileBody, body)
	assert.Equal(t, profileETag, etag)
	assert.False(t, notModified)
}

// A store with no serving snapshot yet has no servable profile: the service
// says so instead of returning an empty body the plugin would serve.
func TestFetchProfile_NoSnapshotYetIsNotReady(t *testing.T) {
	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return &ConnectorModel{ID: "conn-1", StoreID: "store-1", Platform: woocommerce.PlatformName}, nil
		},
	}
	profiles := &mockProfileSource{
		getProfileFunc: func(ctx context.Context, id string) ([]byte, string, error) { return nil, "", nil },
	}
	svc := newTestService(repo, &mockEventBus{}, profiles)

	_, _, _, err := svc.FetchProfile(context.Background(), "conn-1", "")
	require.ErrorIs(t, err, ErrProfileNotReady)
}

func TestFetchProfileNotModified(t *testing.T) {
	model := &ConnectorModel{
		ID:       "conn-1",
		StoreID:  "store-1",
		Platform: woocommerce.PlatformName,
	}

	profileETag := "abc123"

	profiles := &mockProfileSource{
		getProfileFunc: func(ctx context.Context, id string) ([]byte, string, error) {
			return []byte(`{"ucp": {}}`), profileETag, nil
		},
	}

	repo := &mockRepository{
		getByConnectorFunc: func(ctx context.Context, connectorID string) (*ConnectorModel, error) {
			return model, nil
		},
	}

	svc := newTestService(repo, &mockEventBus{}, profiles)

	// Request with matching ETag
	body, etag, notModified, err := svc.FetchProfile(context.Background(), "conn-1", profileETag)
	require.NoError(t, err)
	assert.Nil(t, body)
	assert.Equal(t, profileETag, etag)
	assert.True(t, notModified)
}
