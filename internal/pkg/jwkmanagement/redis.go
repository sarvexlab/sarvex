// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	kp "github.com/sarvexlab/sarvex/internal/pkg/keypair"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
)

// redisKeypairRecord holds either a plaintext private key scalar
// (PrivateKeyD) or an encrypted one (PrivateKeyCiphertext + KMSKeyVersion,
// wrapped by the Encryptor the service was constructed with — Vault
// Transit). Never both.
type redisKeypairRecord struct {
	ID                   string    `json:"id"`
	MerchantID           string    `json:"merchant_id"`
	Kid                  string    `json:"kid"`
	PrivateKeyD          string    `json:"private_key_d,omitempty"`
	PrivateKeyCiphertext string    `json:"private_key_ciphertext,omitempty"`
	KMSKeyVersion        string    `json:"kms_key_version,omitempty"`
	PublicKeyX           string    `json:"public_key_x"`
	PublicKeyY           string    `json:"public_key_y"`
	CreatedAt            time.Time `json:"created_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	Status               string    `json:"status"`
}

type redisService struct {
	client    *redis.Client
	encryptor Encryptor // nil means store the private key in the clear
	log       *slog.Logger
}

func newRedisService(cfg Config, encryptor Encryptor, log *slog.Logger) (JWKManagementService, error) {
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("redis address is required for redis JWK management mode")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	if log == nil {
		log = slog.Default()
	}
	return &redisService{client: client, encryptor: encryptor, log: log}, nil
}

func (s *redisService) keypairKey(storeID, keypairID string) string {
	return fmt.Sprintf("jwk:keypair:%s:%s", storeID, keypairID)
}

func (s *redisService) merchantListKey(storeID string) string {
	return fmt.Sprintf("jwk:merchant:%s", storeID)
}

func (s *redisService) GenerateKeypair(ctx context.Context, storeID string, expiresIn time.Duration) (*JWKKeypair, error) {
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

	now := time.Now()
	kpair := &JWKKeypair{
		ID:         kid,
		StoreID:    storeID,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
		JWK:        jwkModel,
		CreatedAt:  now,
		ExpiresAt:  now.Add(expiresIn),
		Status:     "active",
	}

	if err := s.StoreKeypair(ctx, kpair); err != nil {
		return nil, err
	}

	return kpair, nil
}

func (s *redisService) GetActiveKeypair(ctx context.Context, storeID string) (*JWKKeypair, error) {
	keypairs, err := s.ListInstanceKeypairs(ctx, storeID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	for _, kp := range keypairs {
		if kp.Status == "active" && kp.ExpiresAt.After(now) {
			return kp, nil
		}
	}

	return nil, nil
}

func (s *redisService) ListInstanceKeypairs(ctx context.Context, storeID string) ([]*JWKKeypair, error) {
	listKey := s.merchantListKey(storeID)
	ids, err := s.client.SMembers(ctx, listKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list keypair IDs: %w", err)
	}

	result := make([]*JWKKeypair, 0, len(ids))
	for _, id := range ids {
		kp, err := s.loadKeypair(ctx, storeID, id)
		if err != nil {
			s.log.Error("failed to load keypair, skipping", "error", err, "store_id", storeID, "kid", id)
			continue
		}
		result = append(result, kp)
	}

	return result, nil
}

func (s *redisService) StoreKeypair(ctx context.Context, kpair *JWKKeypair) error {
	if kpair.PrivateKey == nil {
		return fmt.Errorf("private key is required")
	}

	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	kpair.PublicKey.X.FillBytes(xBytes)
	kpair.PublicKey.Y.FillBytes(yBytes)

	record := &redisKeypairRecord{
		ID:         kpair.ID,
		MerchantID: kpair.StoreID,
		Kid:        kpair.JWK.Kid,
		PublicKeyX: base64.RawURLEncoding.EncodeToString(xBytes),
		PublicKeyY: base64.RawURLEncoding.EncodeToString(yBytes),
		CreatedAt:  kpair.CreatedAt,
		ExpiresAt:  kpair.ExpiresAt,
		Status:     kpair.Status,
	}

	if s.encryptor != nil {
		ciphertext, kmsKeyVersion, err := s.encryptor.encryptPrivateKeyD(ctx, kpair.PrivateKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt private key: %w", err)
		}
		record.PrivateKeyCiphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
		record.KMSKeyVersion = kmsKeyVersion
	} else {
		dBytes := make([]byte, 32)
		kpair.PrivateKey.D.FillBytes(dBytes)
		record.PrivateKeyD = base64.RawURLEncoding.EncodeToString(dBytes)
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal keypair: %w", err)
	}

	keypairKey := s.keypairKey(kpair.StoreID, kpair.ID)
	ttl := time.Until(kpair.ExpiresAt)
	if ttl <= 0 {
		ttl = 365 * 24 * time.Hour
	}

	if err := s.client.Set(ctx, keypairKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store keypair: %w", err)
	}

	listKey := s.merchantListKey(kpair.StoreID)
	if err := s.client.SAdd(ctx, listKey, kpair.ID).Err(); err != nil {
		return fmt.Errorf("failed to add keypair ID to merchant list: %w", err)
	}

	return nil
}

func (s *redisService) RotateKeypair(ctx context.Context, storeID string) (*JWKKeypair, error) {
	keypairs, err := s.ListInstanceKeypairs(ctx, storeID)
	if err != nil {
		return nil, err
	}

	for _, kp := range keypairs {
		if kp.Status == "active" {
			kp.Status = "rotated"
			if err := s.StoreKeypair(ctx, kp); err != nil {
				return nil, fmt.Errorf("failed to update active keypair status: %w", err)
			}
			break
		}
	}

	return s.GenerateKeypair(ctx, storeID, 365*24*time.Hour)
}

func (s *redisService) loadKeypair(ctx context.Context, storeID, id string) (*JWKKeypair, error) {
	key := s.keypairKey(storeID, id)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("keypair not found")
		}
		return nil, fmt.Errorf("failed to get keypair: %w", err)
	}

	var record redisKeypairRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal keypair: %w", err)
	}

	return s.recordToKeypair(ctx, &record)
}

func (s *redisService) recordToKeypair(ctx context.Context, record *redisKeypairRecord) (*JWKKeypair, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(record.PublicKeyX)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key X: %w", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(record.PublicKeyY)
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key Y: %w", err)
	}

	var privKey *ecdsa.PrivateKey
	switch {
	case record.PrivateKeyCiphertext != "":
		if s.encryptor == nil {
			return nil, fmt.Errorf("keypair record is encrypted but no encryptor is configured")
		}
		ciphertext, err := base64.RawURLEncoding.DecodeString(record.PrivateKeyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("failed to decode private key ciphertext: %w", err)
		}
		privKey, err = s.encryptor.decryptPrivateKeyD(ctx, ciphertext)
		if err != nil {
			return nil, err
		}
	case record.PrivateKeyD != "":
		dBytes, err := base64.RawURLEncoding.DecodeString(record.PrivateKeyD)
		if err != nil {
			return nil, fmt.Errorf("failed to decode private key: %w", err)
		}
		privKey = &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()},
			D:         bytesToBigInt(dBytes),
		}
	default:
		return nil, fmt.Errorf("keypair record has neither plaintext nor encrypted private key")
	}

	publicKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     bytesToBigInt(xBytes),
		Y:     bytesToBigInt(yBytes),
	}

	jwkModel := &kp.JWKModel{
		Kid: record.Kid,
		Kty: "EC",
		Crv: "P-256",
		X:   record.PublicKeyX,
		Y:   record.PublicKeyY,
		Use: "sig",
		Alg: "ES256", // RFC 7518 name; case-sensitive (RFC 7517 §4.4)
	}

	return &JWKKeypair{
		ID:         record.ID,
		StoreID:    record.MerchantID,
		PrivateKey: privKey,
		PublicKey:  publicKey,
		JWK:        jwkModel,
		CreatedAt:  record.CreatedAt,
		ExpiresAt:  record.ExpiresAt,
		Status:     record.Status,
	}, nil
}
