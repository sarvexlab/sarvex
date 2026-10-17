// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package secretmanagement stores per-store connector credentials (and
// pairing HMAC secrets) in HashiCorp Vault's KV v2 engine. Mongo only ever
// holds a reference to a secret version — never the plaintext, never a
// locally derived ciphertext. Local development, CI and production all use
// the same Vault-backed code path.
package secretmanagement

import (
	"context"
	"fmt"
)

// Config holds configuration for the SecretManagementService.
type Config struct {
	// VaultAddr/VaultToken/VaultMountPath configure the Vault backend.
	// VaultToken is normally left empty in production — the Vault Agent
	// Injector supplies one, same pattern as
	// internal/pkg/jwkmanagement/vault.go. VaultMountPath defaults to
	// "secret" (Vault's conventional KV v2 mount point) if empty.
	VaultAddr      string
	VaultToken     string
	VaultMountPath string
}

// EncryptedConfig is a reference to a secret version in Vault.
// The plaintext config lives only in Vault; this is the value
// persisted in Mongo.
type EncryptedConfig struct {
	SecretName string `bson:"secret_name" json:"secret_name"` // secret path
	Version    string `bson:"version" json:"version"`         // secret path + version
}

// SecretManagementService stores and retrieves config maps as secrets.
// Each Encrypt call creates a new Vault KV v2 secret (with one version)
// and returns a reference to it; Decrypt resolves that reference back to
// the plaintext config.
type SecretManagementService interface {
	// Encrypt stores config as a new secret and returns a reference to it.
	Encrypt(ctx context.Context, config map[string]any) (*EncryptedConfig, error)

	// Decrypt resolves an encrypted config reference back to plaintext.
	Decrypt(ctx context.Context, encrypted *EncryptedConfig) (map[string]any, error)
}

// New creates a SecretManagementService backed by Vault KV v2.
func New(_ context.Context, cfg Config) (SecretManagementService, error) {
	if cfg.VaultAddr == "" {
		return nil, fmt.Errorf("VAULT_ADDR is required for secret management")
	}
	return newVaultService(cfg)
}
