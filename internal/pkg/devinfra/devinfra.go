// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package devinfra resolves the addresses of the backing services that local
// development, the simulator, CI and the test suites share: NATS JetStream
// (event bus) and Vault (Transit signing key + KV v2 secrets). They are the
// same services a self-hosted Kubernetes deployment runs, started locally by
// deployments/docker-compose.yaml, so there is nothing to emulate.
//
// Every value reads its environment variable and falls back to the
// docker-compose default.
package devinfra

import (
	"os"

	"github.com/sarvexlab/sarvex/internal/pkg/jwkmanagement"
	"github.com/sarvexlab/sarvex/internal/pkg/secretmanagement"
)

const (
	// DefaultNATSURL is the compose NATS JetStream address.
	DefaultNATSURL = "nats://localhost:4222"
	// DefaultVaultAddr is the compose Vault address.
	DefaultVaultAddr = "http://localhost:8200"
	// DefaultVaultToken is the compose Vault root token. Local use only.
	DefaultVaultToken = "sarvex-dev-token" //nolint:gosec // G101: local compose dev token, not a secret
	// DefaultVaultTransitKey is the Transit key the compose vault-init job
	// creates for store signing keys.
	DefaultVaultTransitKey = "sarvex-jwk-signing"
)

// NATSURL returns NATS_URL or the compose default.
func NATSURL() string { return env("NATS_URL", DefaultNATSURL) }

// VaultAddr returns VAULT_ADDR or the compose default.
func VaultAddr() string { return env("VAULT_ADDR", DefaultVaultAddr) }

// VaultToken returns VAULT_TOKEN or the compose default.
func VaultToken() string { return env("VAULT_TOKEN", DefaultVaultToken) }

// VaultTransitKey returns VAULT_TRANSIT_KEY_NAME or the compose default.
func VaultTransitKey() string { return env("VAULT_TRANSIT_KEY_NAME", DefaultVaultTransitKey) }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// SecretConfig is the secretmanagement.Config for the local Vault.
func SecretConfig() secretmanagement.Config {
	return secretmanagement.Config{VaultAddr: VaultAddr(), VaultToken: VaultToken()}
}

// JWKConfig is a Redis-backed jwkmanagement.Config whose signing keys are
// wrapped by the local Vault's Transit key.
func JWKConfig(redisAddr string) jwkmanagement.Config {
	return jwkmanagement.Config{
		Mode:                jwkmanagement.ModeRedis,
		RedisAddr:           redisAddr,
		VaultAddr:           VaultAddr(),
		VaultToken:          VaultToken(),
		VaultTransitKeyName: VaultTransitKey(),
	}
}
