// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

// JWK is a JSON Web Key as defined in RFC 7517, restricted to the EC keys
// UCP supports (P-256 / P-384).
type JWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d,omitempty"` // private scalar; only present for private keys
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

// Curve returns the elliptic curve for this key.
func (j JWK) Curve() (elliptic.Curve, error) {
	switch j.Crv {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	default:
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("unsupported curve %q", j.Crv))
	}
}

// HashSize returns the hash output size in bytes (used for r||s width).
func (j JWK) coordLen() (int, error) {
	switch j.Crv {
	case "P-256":
		return 32, nil
	case "P-384":
		return 48, nil
	default:
		return 0, ErrAlgorithmUnsupported.withCause(fmt.Errorf("unsupported curve %q", j.Crv))
	}
}

// PublicKey decodes the JWK to a crypto/ecdsa public key.
func (j JWK) PublicKey() (*ecdsa.PublicKey, error) {
	if j.Kty != "EC" {
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("unsupported kty %q", j.Kty))
	}
	curve, err := j.Curve()
	if err != nil {
		return nil, err
	}
	x, err := base64URLDecodeBigInt(j.X)
	if err != nil {
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("invalid x: %w", err))
	}
	y, err := base64URLDecodeBigInt(j.Y)
	if err != nil {
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("invalid y: %w", err))
	}
	if !curve.IsOnCurve(x, y) {
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("point not on curve"))
	}
	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

// PrivateKey decodes the JWK to a crypto/ecdsa private key. The JWK must
// include the private scalar "d".
func (j JWK) PrivateKey() (*ecdsa.PrivateKey, error) {
	pub, err := j.PublicKey()
	if err != nil {
		return nil, err
	}
	if j.D == "" {
		return nil, fmt.Errorf("jwk: missing private scalar d")
	}
	d, err := base64URLDecodeBigInt(j.D)
	if err != nil {
		return nil, fmt.Errorf("jwk: invalid d: %w", err)
	}
	return &ecdsa.PrivateKey{PublicKey: *pub, D: d}, nil
}

// JWKFromPrivateKey builds a JWK from an ECDSA private key. Use this when
// generating keys and publishing the public half in /.well-known/ucp.
func JWKFromPrivateKey(kid string, pk *ecdsa.PrivateKey) (JWK, error) {
	jwk, err := JWKFromPublicKey(kid, &pk.PublicKey)
	if err != nil {
		return JWK{}, err
	}
	clen := byteLenForCurve(pk.PublicKey.Curve)
	jwk.D = base64.RawURLEncoding.EncodeToString(leftPad(pk.D.Bytes(), clen))
	return jwk, nil
}

// JWKFromPublicKey builds a public-only JWK.
func JWKFromPublicKey(kid string, pk *ecdsa.PublicKey) (JWK, error) {
	var crv, alg string
	switch pk.Curve.Params().BitSize {
	case 256:
		crv, alg = "P-256", "ES256"
	case 384:
		crv, alg = "P-384", "ES384"
	default:
		return JWK{}, ErrAlgorithmUnsupported.withCause(fmt.Errorf("unsupported curve bit size %d", pk.Curve.Params().BitSize))
	}
	clen := byteLenForCurve(pk.Curve)
	return JWK{
		Kid: kid,
		Kty: "EC",
		Crv: crv,
		Alg: alg,
		Use: "sig",
		X:   base64.RawURLEncoding.EncodeToString(leftPad(pk.X.Bytes(), clen)),
		Y:   base64.RawURLEncoding.EncodeToString(leftPad(pk.Y.Bytes(), clen)),
	}, nil
}

// Profile is the subset of /.well-known/ucp this package needs: the
// profile's public signing keys. Spec generations publish them under
// different names — "signing_keys" (2026-04-08), then "keys" (2026-08-25, a
// JWK Set) — and a counterparty's profile may be in any generation, so both
// are read. Implementations may embed it inside a richer profile struct.
type Profile struct {
	SigningKeys []JWK `json:"signing_keys,omitempty"`
	Keys        []JWK `json:"keys,omitempty"`
}

// FindKey returns the signing key matching kid, or nil if none.
func (p Profile) FindKey(kid string) *JWK {
	for _, set := range [][]JWK{p.Keys, p.SigningKeys} {
		for i := range set {
			if set[i].Kid == kid {
				return &set[i]
			}
		}
	}
	return nil
}

// hasKeys reports whether the profile publishes any signing key.
func (p Profile) hasKeys() bool { return len(p.Keys) > 0 || len(p.SigningKeys) > 0 }

// MarshalJSON for Profile is a passthrough; provided to make the type explicit.
func (p Profile) MarshalJSON() ([]byte, error) {
	type alias Profile
	return json.Marshal(alias(p))
}

func base64URLDecodeBigInt(s string) (*big.Int, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Some implementations use std URL encoding with padding.
		b2, err2 := base64.URLEncoding.DecodeString(s)
		if err2 != nil {
			return nil, err
		}
		b = b2
	}
	return new(big.Int).SetBytes(b), nil
}

func byteLenForCurve(c elliptic.Curve) int {
	return (c.Params().BitSize + 7) / 8
}

func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}
