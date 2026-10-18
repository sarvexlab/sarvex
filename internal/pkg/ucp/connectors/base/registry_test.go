// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package base_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// ─── test doubles ────────────────────────────────────────────────────────────

type stubInstance struct {
	storeID  string
	platform domain.CommerceSystemType
}

func (s *stubInstance) StoreID() string                                   { return s.storeID }
func (s *stubInstance) Version() string                                   { return "2024-01-01" }
func (s *stubInstance) Services() map[string][]wire.Service               { return nil }
func (s *stubInstance) Capabilities() map[string][]wire.Capability        { return nil }
func (s *stubInstance) PaymentHandlers() map[string][]wire.PaymentHandler { return nil }
func (s *stubInstance) SigningKeys() []wire.JWK                           { return nil }
func (s *stubInstance) StorefrontURL() string                             { return "" }
func (s *stubInstance) Configuration() ucp.Configuration {
	return ucp.Configuration{
		MerchantID: "merchant-1",
		StoreID:    s.storeID,
		Platform:   s.platform,
	}
}

// stubConnector embeds a nil base.Connector so it satisfies the full
// interface (including the store-API pull methods) without boilerplate;
// only the methods these tests actually exercise are overridden below.
type stubConnector struct {
	base.Connector
	id string
}

func (s *stubConnector) CatalogSearch(_ context.Context, _ string, _ *base.CatalogSearchRequest) (*base.CatalogSearchResponse, error) {
	return nil, nil
}
func (s *stubConnector) CatalogLookup(_ context.Context, _ string, _ string) (*base.CatalogLookupResponse, error) {
	return nil, nil
}
func (s *stubConnector) CartCreate(_ context.Context, _ string, _ *base.CreateCartRequest) (*base.CartResponse, error) {
	return nil, nil
}
func (s *stubConnector) CartGet(_ context.Context, _ string, _ string) (*base.CartResponse, error) {
	return nil, nil
}
func (s *stubConnector) CartUpdate(_ context.Context, _ string, _ string, _ *base.UpdateCartRequest) (*base.CartResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutCreate(_ context.Context, _ string, _ *base.CreateCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutGet(_ context.Context, _ string, _ string) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutUpdate(_ context.Context, _ string, _ string, _ *base.UpdateCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutComplete(_ context.Context, _ string, _ string, _ *base.CompleteCheckoutRequest) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutCancel(_ context.Context, _ string, _ string) (*base.CheckoutResponse, error) {
	return nil, nil
}
func (s *stubConnector) CheckoutURL(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}
func (s *stubConnector) OrderGet(_ context.Context, _ string, _ string) (*base.OrderEvent, error) {
	return nil, nil
}
func (s *stubConnector) UpdateProduct(_ context.Context, _ string, _ string, _ map[string]interface{}) error {
	return nil // stub: no-op for testing
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTestRegistry() *base.Registry {
	noCreds := base.CredentialsFunc(func(_ context.Context, _ string) (map[string]any, error) {
		return nil, errors.New("no credentials provider configured for this test")
	})
	return base.New(noCreds, slog.Default())
}

func shopifyInstance(id string) *stubInstance {
	return &stubInstance{storeID: id, platform: domain.CommerceSystemType("shopify")}
}

// ─── tests ───────────────────────────────────────────────────────────────────

func TestRegistry_Get_NoFactoryRegistered(t *testing.T) {
	r := newTestRegistry()
	_, err := r.Get(shopifyInstance("inst-1"))
	if err == nil {
		t.Fatal("expected error when no factory is registered")
	}
}

func TestRegistry_Get_FactoryError(t *testing.T) {
	r := newTestRegistry()
	wantErr := errors.New("factory boom")
	r.RegisterFactory("shopify", func(_ base.MerchantConfig, _ *slog.Logger) (base.Connector, error) {
		return nil, wantErr
	})
	_, err := r.Get(shopifyInstance("inst-1"))
	if err == nil {
		t.Fatal("expected error propagated from factory")
	}
}

func TestRegistry_Get_CachesConnectorOnFirstCall(t *testing.T) {
	r := newTestRegistry()
	calls := 0
	r.RegisterFactory("shopify", func(_ base.MerchantConfig, _ *slog.Logger) (base.Connector, error) {
		calls++
		return &stubConnector{id: "from-factory"}, nil
	})

	inst := shopifyInstance("inst-1")
	c1, err := r.Get(inst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c2, err := r.Get(inst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c1 != c2 {
		t.Fatal("expected same connector instance from cache")
	}
	if calls != 1 {
		t.Fatalf("factory called %d times, want 1", calls)
	}
}

func TestRegistry_Get_CacheKeyIsPerInstance(t *testing.T) {
	r := newTestRegistry()
	r.RegisterFactory("shopify", func(cfg base.MerchantConfig, _ *slog.Logger) (base.Connector, error) {
		return &stubConnector{id: cfg.StoreID}, nil
	})

	c1, _ := r.Get(shopifyInstance("inst-A"))
	c2, _ := r.Get(shopifyInstance("inst-B"))
	if c1 == c2 {
		t.Fatal("different instances should return different connectors")
	}
}

func TestRegistry_Set_OverridesFactory(t *testing.T) {
	r := newTestRegistry()
	stub := &stubConnector{id: "injected"}
	r.Set("shopify", "inst-1", stub)

	got, err := r.Get(shopifyInstance("inst-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != stub {
		t.Fatal("expected injected stub to be returned")
	}
}

func TestRegistry_Set_DoesNotAffectOtherInstances(t *testing.T) {
	r := newTestRegistry()
	r.RegisterFactory("shopify", func(_ base.MerchantConfig, _ *slog.Logger) (base.Connector, error) {
		return &stubConnector{id: "from-factory"}, nil
	})
	r.Set("shopify", "inst-injected", &stubConnector{id: "injected"})

	got, err := r.Get(shopifyInstance("inst-other"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s, ok := got.(*stubConnector); !ok || s.id != "from-factory" {
		t.Fatalf("expected factory connector for uninjected instance, got %+v", got)
	}
}

func TestRegistry_Get_ConcurrentSafe(t *testing.T) {
	r := newTestRegistry()
	r.RegisterFactory("shopify", func(_ base.MerchantConfig, _ *slog.Logger) (base.Connector, error) {
		return &stubConnector{}, nil
	})
	inst := shopifyInstance("inst-1")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Get(inst); err != nil {
				t.Errorf("concurrent Get failed: %v", err)
			}
		}()
	}
	wg.Wait()
}
