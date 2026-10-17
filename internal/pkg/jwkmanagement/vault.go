// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"fmt"

	vaultapi "github.com/hashicorp/vault/api"
)

// vaultEncryptor wraps a store's ECDSA private key scalar with a Vault
// Transit key before it's persisted (see deployments/selfhost's Vault Helm
// release and deployments/docker-compose.yaml for local development).
// Callers (redis.go/mongo.go) depend only on the Encryptor interface; the
// ciphertext is Vault's own "vault:v<version>:<base64>" string, stored as
// raw bytes.
type vaultEncryptor struct {
	client  *vaultapi.Logical
	keyName string
}

var _ Encryptor = (*vaultEncryptor)(nil)

// vaultConfigured reports whether cfg carries a full Vault Transit key
// identifier.
func vaultConfigured(cfg Config) bool {
	return cfg.VaultAddr != "" && cfg.VaultTransitKeyName != ""
}

// newVaultEncryptor builds a Vault API client pointed at cfg.VaultAddr and
// resolves the Transit key name from cfg. It does not create the key —
// Vault's Transit key is expected to already exist (deployments/selfhost's
// bootstrap-cluster.sh and the local vault-init job in
// deployments/docker-compose.yaml both enable the Transit engine and create
// it: `vault secrets enable transit && vault write -f
// transit/keys/<VaultTransitKeyName>`).
func newVaultEncryptor(cfg Config) (*vaultEncryptor, error) {
	vcfg := vaultapi.DefaultConfig()
	vcfg.Address = cfg.VaultAddr
	client, err := vaultapi.NewClient(vcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Vault client: %w", err)
	}
	if cfg.VaultToken != "" {
		client.SetToken(cfg.VaultToken)
	}
	// Otherwise falls back to the Vault Agent Injector's rendered token file
	// (VAULT_TOKEN env var / ~/.vault-token), so no secret lives in config.

	return &vaultEncryptor{client: client.Logical(), keyName: cfg.VaultTransitKeyName}, nil
}

// encryptPrivateKeyD encrypts the raw ECDSA private key scalar with the
// Transit key and returns Vault's ciphertext string (as raw bytes) plus the
// key version that performed the encryption.
func (e *vaultEncryptor) encryptPrivateKeyD(ctx context.Context, privKey *ecdsa.PrivateKey) ([]byte, string, error) {
	dBytes := make([]byte, 32)
	privKey.D.FillBytes(dBytes)

	secret, err := e.client.WriteWithContext(ctx, fmt.Sprintf("transit/encrypt/%s", e.keyName), map[string]interface{}{
		"plaintext": base64.StdEncoding.EncodeToString(dBytes),
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to encrypt private key with vault transit: %w", err)
	}
	ciphertext, ok := secret.Data["ciphertext"].(string)
	if !ok {
		return nil, "", fmt.Errorf("vault transit encrypt response missing ciphertext")
	}

	// Vault's ciphertext string embeds its own key version
	// ("vault:v3:base64…") — surface it directly as KMSKeyVersion rather
	// than making a second API call.
	return []byte(ciphertext), ciphertext, nil
}

// decryptPrivateKeyD decrypts a Vault-wrapped ECDSA private key scalar and
// rebuilds the private key (Curve and D only — callers fill in PublicKey
// separately from the store's persisted X/Y).
func (e *vaultEncryptor) decryptPrivateKeyD(ctx context.Context, ciphertext []byte) (*ecdsa.PrivateKey, error) {
	secret, err := e.client.WriteWithContext(ctx, fmt.Sprintf("transit/decrypt/%s", e.keyName), map[string]interface{}{
		"ciphertext": string(ciphertext),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt private key with vault transit: %w", err)
	}
	plaintextB64, ok := secret.Data["plaintext"].(string)
	if !ok {
		return nil, fmt.Errorf("vault transit decrypt response missing plaintext")
	}
	dBytes, err := base64.StdEncoding.DecodeString(plaintextB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode vault transit plaintext: %w", err)
	}

	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
		},
		D: bytesToBigInt(dBytes),
	}, nil
}
