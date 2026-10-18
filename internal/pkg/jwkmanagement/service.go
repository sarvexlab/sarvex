// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"time"

	"github.com/sarvexlab/sarvex/internal/pkg/keypair"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

type Mode string

const (
	ModeRedis Mode = "redis"
	ModeMongo Mode = "mongo"
)

type Config struct {
	Mode          Mode
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	Repository    keypair.Repository

	// VaultAddr/VaultToken/VaultTransitKeyName configure the Vault
	// Transit-backed Encryptor (vault.go; see deployments/selfhost).
	// VaultToken is normally left empty in production: the Vault Agent
	// Injector renders a token file the client picks up on its own.
	VaultAddr           string
	VaultToken          string
	VaultTransitKeyName string

	// RequireEncryption, when true, makes NewFromConfig fail closed instead
	// of falling back to storing the private key scalar in the clear when
	// Vault Transit is not configured. Callers should set
	// this from their own "is this production" check (e.g. Env ==
	// "production") — jwkmanagement itself has no concept of environments,
	// only of whether an Encryptor is required.
	RequireEncryption bool
}

type JWKKeypair struct {
	ID            string
	StoreID       string
	PrivateKey    *ecdsa.PrivateKey
	PublicKey     *ecdsa.PublicKey
	JWK           *keypair.JWKModel
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Status        string
	KMSKeyVersion string
}

type JWKManagementService interface {
	GenerateKeypair(ctx context.Context, storeID string, expiresIn time.Duration) (*JWKKeypair, error)
	GetActiveKeypair(ctx context.Context, storeID string) (*JWKKeypair, error)
	ListInstanceKeypairs(ctx context.Context, storeID string) ([]*JWKKeypair, error)
	StoreKeypair(ctx context.Context, keypair *JWKKeypair) error
	RotateKeypair(ctx context.Context, storeID string) (*JWKKeypair, error)
}

// PublicWireJWKs returns the keypair's public key as the wire JWK list published
// in a store's UCP profile, or an empty slice when the keypair or its public JWK
// is absent.
func PublicWireJWKs(kp *JWKKeypair) []wire.JWK {
	if kp == nil || kp.JWK == nil {
		return []wire.JWK{}
	}
	return []wire.JWK{{
		Kid: kp.JWK.Kid,
		Kty: kp.JWK.Kty,
		Crv: kp.JWK.Crv,
		X:   kp.JWK.X,
		Y:   kp.JWK.Y,
		Use: kp.JWK.Use,
		Alg: kp.JWK.Alg,
	}}
}

// NewFromConfig constructs a JWK management service from configuration,
// choosing the storage backend by Mode. It defaults to ModeRedis when unset.
//
// Cross-process key sharing (e.g. the control plane provisions a key the
// serving plane must read) requires a shared backend: ModeRedis. ModeMongo
// currently has no working keypair repository wired in (keypair.Repository has
// no Mongo implementation yet) and will error — callers that need durable
// cross-plane keys must use ModeRedis until then.
//
// Regardless of storage backend, the private key is encrypted through
// Vault Transit (vault.go) when VaultAddr/VaultTransitKeyName are set.
// ModeRedis falls back to storing the key in the clear only when Vault is
// not configured AND cfg.RequireEncryption is false — set
// RequireEncryption from your own "is this production" check to make that
// combination a startup error instead of a silent plaintext fallback.
func NewFromConfig(cfg Config, log *slog.Logger) (JWKManagementService, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeRedis
	}

	encryptor, err := newEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	switch cfg.Mode {
	case ModeRedis:
		if encryptor == nil && cfg.RequireEncryption {
			return nil, fmt.Errorf("jwkmanagement: RequireEncryption is set but Vault Transit is not configured")
		}
		return newRedisService(cfg, encryptor, log)
	case ModeMongo:
		if cfg.Repository == nil {
			return nil, fmt.Errorf("keypair repository is required for ModeMongo")
		}
		if encryptor == nil {
			return nil, fmt.Errorf("jwkmanagement: ModeMongo requires Vault Transit to be configured")
		}
		return newMongoService(cfg, encryptor)
	default:
		return nil, fmt.Errorf("unknown jwk management mode: %s", cfg.Mode)
	}
}

// newEncryptor returns the Vault Transit Encryptor, or (nil, nil) when Vault
// is not configured — the caller decides whether that's acceptable (see
// RequireEncryption).
func newEncryptor(cfg Config) (Encryptor, error) {
	if vaultConfigured(cfg) {
		return newVaultEncryptor(cfg)
	}
	return nil, nil
}
