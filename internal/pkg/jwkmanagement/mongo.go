// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	kp "github.com/sarvexlab/sarvex/internal/pkg/keypair"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
)

// mongoService persists keypairs in MongoDB, with the private key always
// encrypted before it's written (see vault.go).
type mongoService struct {
	repo      kp.Repository
	cfg       Config
	encryptor Encryptor
}

func newMongoService(cfg Config, encryptor Encryptor) (JWKManagementService, error) {
	return &mongoService{
		repo:      cfg.Repository,
		cfg:       cfg,
		encryptor: encryptor,
	}, nil
}

func (s *mongoService) GenerateKeypair(ctx context.Context, storeID string, expiresIn time.Duration) (*JWKKeypair, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	publicKey := &privateKey.PublicKey
	kid := uuid.New().String()

	jwkDomain := ucpsig.ECPublicKeyToJWK(publicKey, kid)

	jwkModel := &kp.JWKModel{
		Kid: jwkDomain.Kid,
		Kty: jwkDomain.Kty,
		Crv: jwkDomain.Crv,
		X:   jwkDomain.X,
		Y:   jwkDomain.Y,
		Use: jwkDomain.Use,
		Alg: jwkDomain.Alg,
	}

	ciphertext, kmsKeyVersion, err := s.encryptor.encryptPrivateKeyD(ctx, privateKey)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	publicKey.X.FillBytes(xBytes)
	publicKey.Y.FillBytes(yBytes)

	mongoModel := &kp.MerchantKeypairModel{
		ID:                   kid,
		MerchantID:           storeID,
		Kid:                  kid,
		CreatedAt:            now,
		ExpiresAt:            now.Add(expiresIn),
		Status:               "active",
		PublicKeyX:           base64.RawURLEncoding.EncodeToString(xBytes),
		PublicKeyY:           base64.RawURLEncoding.EncodeToString(yBytes),
		PrivateKeyCiphertext: ciphertext,
		KMSKeyVersion:        kmsKeyVersion,
	}

	if err := s.repo.Save(ctx, mongoModel); err != nil {
		return nil, fmt.Errorf("failed to save keypair: %w", err)
	}

	keypair := &JWKKeypair{
		ID:         kid,
		StoreID:    storeID,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
		JWK:        jwkModel,
		CreatedAt:  now,
		ExpiresAt:  now.Add(expiresIn),
		Status:     "active",
	}

	return keypair, nil
}

func (s *mongoService) GetActiveKeypair(ctx context.Context, storeID string) (*JWKKeypair, error) {
	mongoModel, err := s.repo.GetActiveKeypair(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active keypair from repository: %w", err)
	}

	if mongoModel == nil {
		return nil, nil
	}

	return s.mongoModelToKeypair(ctx, mongoModel)
}

func (s *mongoService) ListInstanceKeypairs(ctx context.Context, storeID string) ([]*JWKKeypair, error) {
	mongoModels, err := s.repo.GetByMerchantID(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list keypairs: %w", err)
	}

	result := make([]*JWKKeypair, 0, len(mongoModels))
	for _, mongoModel := range mongoModels {
		keypair, err := s.mongoModelToKeypair(ctx, mongoModel)
		if err != nil {
			return nil, err
		}
		result = append(result, keypair)
	}

	return result, nil
}

func (s *mongoService) StoreKeypair(ctx context.Context, keypair *JWKKeypair) error {
	if keypair.PrivateKey == nil {
		return fmt.Errorf("private key is required")
	}

	ciphertext, kmsKeyVersion, err := s.encryptor.encryptPrivateKeyD(ctx, keypair.PrivateKey)
	if err != nil {
		return err
	}

	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	keypair.PublicKey.X.FillBytes(xBytes)
	keypair.PublicKey.Y.FillBytes(yBytes)

	mongoModel := &kp.MerchantKeypairModel{
		ID:                   keypair.ID,
		MerchantID:           keypair.StoreID,
		Kid:                  keypair.JWK.Kid,
		CreatedAt:            keypair.CreatedAt,
		ExpiresAt:            keypair.ExpiresAt,
		Status:               keypair.Status,
		PublicKeyX:           base64.RawURLEncoding.EncodeToString(xBytes),
		PublicKeyY:           base64.RawURLEncoding.EncodeToString(yBytes),
		PrivateKeyCiphertext: ciphertext,
		KMSKeyVersion:        kmsKeyVersion,
	}

	return s.repo.Save(ctx, mongoModel)
}

func (s *mongoService) RotateKeypair(ctx context.Context, storeID string) (*JWKKeypair, error) {
	// Get active keypair
	activeKeypair, err := s.GetActiveKeypair(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active keypair: %w", err)
	}

	// Mark as rotated if exists
	if activeKeypair != nil {
		mongoModel, _ := s.repo.GetActiveKeypair(ctx, storeID)
		if mongoModel != nil {
			mongoModel.Status = "rotated"
			if err := s.repo.Save(ctx, mongoModel); err != nil {
				return nil, fmt.Errorf("failed to update keypair status: %w", err)
			}
		}
	}

	// Generate new keypair
	return s.GenerateKeypair(ctx, storeID, 365*24*time.Hour)
}

func (s *mongoService) mongoModelToKeypair(ctx context.Context, mongoModel *kp.MerchantKeypairModel) (*JWKKeypair, error) {
	var privateKey *ecdsa.PrivateKey
	var decryptErr error

	// If a wrapped private key exists, decrypt it
	if mongoModel.PrivateKeyCiphertext != nil {
		privateKey, decryptErr = s.encryptor.decryptPrivateKeyD(ctx, mongoModel.PrivateKeyCiphertext)
	}

	xBytes, _ := base64.RawURLEncoding.DecodeString(mongoModel.PublicKeyX)
	yBytes, _ := base64.RawURLEncoding.DecodeString(mongoModel.PublicKeyY)

	publicKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     bytesToBigInt(xBytes),
		Y:     bytesToBigInt(yBytes),
	}

	jwkModel := &kp.JWKModel{
		Kid: mongoModel.Kid,
		Kty: "EC",
		Crv: "P-256",
		X:   mongoModel.PublicKeyX,
		Y:   mongoModel.PublicKeyY,
		Use: "sig",
		Alg: "ES256", // RFC 7518 name; case-sensitive (RFC 7517 §4.4)
	}

	keypair := &JWKKeypair{
		ID:         mongoModel.ID,
		StoreID:    mongoModel.MerchantID,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
		JWK:        jwkModel,
		CreatedAt:  mongoModel.CreatedAt,
		ExpiresAt:  mongoModel.ExpiresAt,
		Status:     mongoModel.Status,
	}

	// Log decryption error but don't fail - public key is still useful
	if decryptErr != nil {
		fmt.Printf("Warning: failed to decrypt private key: %v\n", decryptErr)
	}

	return keypair, nil
}

func bytesToBigInt(data []byte) *big.Int {
	return new(big.Int).SetBytes(data)
}
