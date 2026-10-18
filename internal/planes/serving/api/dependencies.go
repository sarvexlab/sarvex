// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/sarvexlab/sarvex/internal/platform"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/connectors/base"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
	"github.com/sarvexlab/sarvex/internal/pkg/utils"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// ServingDependencies holds all injectable dependencies for the serving handler.
type ServingDependencies struct {
	Registry           *base.Registry
	NegotiationService *negotiation.NegotiationService
	EventPublisher     eventbus.EventBus
	StoreProvider      ucp.StoreProviderFunc
	SignatureConfig    ucpsig.SignatureConfig
	ServingCache       serving.ServingCache
	RateLimiter        serving.RateLimiter
	// CartStore persists cart records; nil falls back to an in-memory store
	// (single process — fine for tests, not for horizontally scaled serving).
	CartStore  serving.CartStore
	JWKService jwkmanagement.JWKManagementService
	IsProd     bool
}

// FromConfig constructs ServingDependencies from a loaded configuration.
func FromConfig(config *utils.Configurations,
	eventPublisher eventbus.EventBus,
	logger *slog.Logger,
	servingCache serving.ServingCache,
	db *mongo.Database,
	secrets secretmanagement.SecretManagementService,
	redisClient *redis.Client) (ServingDependencies, error) {
	isProd := config.Env == ""

	// ── Registry ────────────────────────────────────────────────────────────────
	// Real credentials, because the serving plane's checkout path makes the
	// one live merchant call the architecture permits and cannot do it
	// without them (checkout_credentials.go). Invariant §4.5.2 — the serving
	// plane reads only the serving cache — is enforced instead by
	// blockPullConnector (pull_guard.go), which errors on the store-API pull
	// methods while letting checkout through.
	//
	// This provider used to be a stub that always errored. base.Registry
	// hands one provider to the whole connector, so refusing credentials
	// refused CheckoutCreate as surely as FetchCatalog, and every agent
	// checkout failed with a 503 before ever reaching the merchant.
	reg := base.New(checkoutOnlyCredentials(db, secrets), logger)
	platform.RegisterConnectors(reg) // Shopify, BigCommerce, Commercetools are frozen: not served

	// ── JWK management ──────────────────────────────────────────────────────────
	// Single source of truth for store signing keys: the serving signer reads the
	// private half here, and the published profile carries the matching public half.
	jwkConfig := jwkmanagement.Config{
		Mode:                jwkmanagement.Mode(config.JWKManagementMode),
		RedisAddr:           config.RedisAddress,
		RedisPassword:       config.RedisPassword,
		RedisDB:             config.RedisDatabase,
		VaultAddr:           config.VaultAddr,
		VaultToken:          config.VaultToken,
		VaultTransitKeyName: config.VaultTransitKeyName,
		RequireEncryption:   config.Env == "production",
	}
	jwkService, err := jwkmanagement.NewFromConfig(jwkConfig, logger)
	if err != nil {
		return ServingDependencies{}, fmt.Errorf("jwk management: %w", err)
	}

	cacheProfileService := serving.NewCacheProfileSource(servingCache)

	provider := func(ctx context.Context, storeID string) (ucp.StoreProvider, error) {
		return cacheProfileService.GetStructuredProfile(ctx, storeID)
	}

	// ── Signature verifier & signer ──────────────────────────────────────────────
	resolver := ucpsig.NewHTTPProfileResolver(&http.Client{Timeout: 3 * time.Second}, 5*time.Minute)
	resolver.AllowInsecureHTTP = true
	if isProd {
		resolver.Trust = func(url string) bool {
			return url == "https://platform.example/.well-known/ucp"
		}
	}
	verifier := ucpsig.NewVerifier(resolver, !isProd)

	// ── Negotiation service ──────────────────────────────────────────────────────
	// ServableVersions() (the generations serving the catalog) covers non-checkout operations —
	// catalog search/lookup render in every servable generation (see
	// internal/spec/vYYYYMMDD/codec/catalog.go). CheckoutServableVersions()
	// (the generations serving checkout) covers checkout, cart and order, rendered per
	// generation at the wireadapt edge (version.go). It is a separate set so the
	// money path can trail catalog while a new generation's checkout support is
	// built; today the two are equal.
	negotiationService := negotiation.NewNegotiationService(
		negotiation.WithStoreProvider(provider),
		negotiation.WithHTTPClient(&http.Client{Timeout: 3 * time.Second}),
		negotiation.WithServableVersions(negotiation.ServableVersions()),
		negotiation.WithCheckoutServableVersions(negotiation.CheckoutServableVersions()),
	)

	signatureConfig := ucpsig.SignatureConfig{
		Disabled:       !isProd,
		Verifier:       verifier,
		SignerProvider: buildEchoSignerProvider(jwkService),
	}

	return ServingDependencies{
		Registry:           reg,
		NegotiationService: negotiationService,
		EventPublisher:     eventPublisher,
		StoreProvider:      provider,
		SignatureConfig:    signatureConfig,
		ServingCache:       servingCache,
		RateLimiter:        serving.NewRedisRateLimiter(redisClient, logger),
		JWKService:         jwkService,
		IsProd:             isProd,
	}, nil
}

// buildEchoSignerProvider constructs the echo.Context-aware signer closure.

func buildEchoSignerProvider(
	jwkService jwkmanagement.JWKManagementService,
) ucpsig.SignerProvider {
	return func(c echo.Context) (*ucpsig.Signer, error) {
		storeID := c.Param("storeID")
		if storeID == "" {
			return nil, fmt.Errorf("missing storeID in path")
		}
		activeKeypair, err := jwkService.GetActiveKeypair(c.Request().Context(), storeID)
		if err != nil {
			return nil, err
		}
		return ucpsig.NewSigner(activeKeypair.PrivateKey, ucpsig.JWK{
			Kid: activeKeypair.JWK.Kid,
			Kty: activeKeypair.JWK.Kty,
			Crv: activeKeypair.JWK.Crv,
			X:   activeKeypair.JWK.X,
			Y:   activeKeypair.JWK.Y,
			Use: activeKeypair.JWK.Use,
			Alg: activeKeypair.JWK.Alg,
		})
	}
}
