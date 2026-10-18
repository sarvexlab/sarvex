// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"crypto/ecdsa"
)

// Encryptor envelope-encrypts a store's ECDSA private key scalar before
// it's persisted, regardless of which storage backend (Redis or Mongo)
// holds the resulting ciphertext. vaultEncryptor (vault.go, HashiCorp Vault
// Transit) implements it; redis.go and mongo.go depend only on this
// interface, never on a concrete backend.
type Encryptor interface {
	encryptPrivateKeyD(ctx context.Context, privKey *ecdsa.PrivateKey) (ciphertext []byte, keyVersion string, err error)
	decryptPrivateKeyD(ctx context.Context, ciphertext []byte) (*ecdsa.PrivateKey, error)
}
