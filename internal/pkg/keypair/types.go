// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package keypair

import (
	"context"
	"time"
)

// Repository defines the interface for persisting merchant keypairs
type Repository interface {
	Save(ctx context.Context, keypair *MerchantKeypairModel) error
	GetByID(ctx context.Context, id string) (*MerchantKeypairModel, error)
	GetByMerchantID(ctx context.Context, merchantID string) ([]*MerchantKeypairModel, error)
	GetActiveKeypair(ctx context.Context, merchantID string) (*MerchantKeypairModel, error)
	Delete(ctx context.Context, id string) error
}

// JWKModel represents a JSON Web Key used for signing
type JWKModel struct {
	Kid string `bson:"kid"`
	Kty string `bson:"kty"`
	Crv string `bson:"crv,omitempty"`
	X   string `bson:"x,omitempty"`
	Y   string `bson:"y,omitempty"`
	Use string `bson:"use,omitempty"`
	Alg string `bson:"alg,omitempty"`
}

// MerchantKeypairModel represents a merchant's signing keypair in MongoDB
type MerchantKeypairModel struct {
	ID                   string    `bson:"_id"`
	MerchantID           string    `bson:"merchant_id"`
	Kid                  string    `bson:"kid"`
	CreatedAt            time.Time `bson:"created_at"`
	ExpiresAt            time.Time `bson:"expires_at"`
	Status               string    `bson:"status"` // "active", "rotated", "revoked"
	PublicKeyX           string    `bson:"public_key_x"`
	PublicKeyY           string    `bson:"public_key_y"`
	PrivateKeyCiphertext []byte    `bson:"private_key_ciphertext,omitempty"`
	KMSKeyVersion        string    `bson:"kms_key_version,omitempty"`
}
