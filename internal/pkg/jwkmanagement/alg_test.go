// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package jwkmanagement

import (
	"context"
	"encoding/base64"
	"testing"

	kp "github.com/sarvexlabs/sarvex.cp/internal/pkg/keypair"
)

// A store's published JWK said "alg": "es256". JWK alg values are
// case-sensitive (RFC 7517 §4.4) and the registered name is "ES256"; the
// 2026-08-25 profile schema pairs P-256 with exactly "ES256".
const wantAlg = "ES256"

var b64 = base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})

func TestRedisRecordToKeypair_PublishesES256(t *testing.T) {
	s := &redisService{}
	got, err := s.recordToKeypair(context.Background(), &redisKeypairRecord{
		Kid: "k1", PublicKeyX: b64, PublicKeyY: b64, PrivateKeyD: b64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.JWK.Alg != wantAlg {
		t.Errorf("alg = %q, want %q", got.JWK.Alg, wantAlg)
	}
}

func TestMongoModelToKeypair_PublishesES256(t *testing.T) {
	s := &mongoService{}
	got, err := s.mongoModelToKeypair(context.Background(), &kp.MerchantKeypairModel{
		Kid: "k1", PublicKeyX: b64, PublicKeyY: b64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.JWK.Alg != wantAlg {
		t.Errorf("alg = %q, want %q", got.JWK.Alg, wantAlg)
	}
}
