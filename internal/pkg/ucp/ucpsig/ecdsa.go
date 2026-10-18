// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucpsig

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"math/big"
)

// hashFor returns the hash function for the given curve per the UCP table.
func hashFor(crv string) (hash.Hash, error) {
	switch crv {
	case "P-256":
		return sha256.New(), nil
	case "P-384":
		// SHA-384 is the SHA-512/384 variant.
		return sha512.New384(), nil
	default:
		return nil, ErrAlgorithmUnsupported.withCause(fmt.Errorf("unsupported curve %q", crv))
	}
}

// SignBase signs the signature base with key priv and returns the raw r||s
// encoded signature (64 bytes for P-256, 96 for P-384). The hash is selected
// from the JWK metadata of jwk; priv must correspond to jwk.
func SignBase(base string, priv *ecdsa.PrivateKey, jwk JWK) ([]byte, error) {
	h, err := hashFor(jwk.Crv)
	if err != nil {
		return nil, err
	}
	clen, err := jwk.coordLen()
	if err != nil {
		return nil, err
	}
	h.Write([]byte(base))
	digest := h.Sum(nil)

	r, s, err := ecdsa.Sign(rand.Reader, priv, digest)
	if err != nil {
		return nil, fmt.Errorf("ucpsig: sign failed: %w", err)
	}

	out := make([]byte, 2*clen)
	copy(out[clen-len(r.Bytes()):clen], r.Bytes())
	copy(out[2*clen-len(s.Bytes()):], s.Bytes())
	return out, nil
}

// VerifyBase verifies a raw r||s signature against the base using the public
// key in jwk.
func VerifyBase(base string, sig []byte, jwk JWK) error {
	pub, err := jwk.PublicKey()
	if err != nil {
		return err
	}
	clen, err := jwk.coordLen()
	if err != nil {
		return err
	}
	if len(sig) != 2*clen {
		return ErrSignatureInvalid.withCause(fmt.Errorf("expected %d-byte signature, got %d", 2*clen, len(sig)))
	}
	h, err := hashFor(jwk.Crv)
	if err != nil {
		return err
	}
	h.Write([]byte(base))
	digest := h.Sum(nil)

	r := new(big.Int).SetBytes(sig[:clen])
	s := new(big.Int).SetBytes(sig[clen:])
	if !ecdsa.Verify(pub, digest, r, s) {
		return ErrSignatureInvalid
	}
	return nil
}

// EncodeSignatureHeader builds the value for the Signature header for a single
// label (typically "sig1").
func EncodeSignatureHeader(label string, sig []byte) string {
	return label + "=:" + base64.StdEncoding.EncodeToString(sig) + ":"
}

func decodeStdBase64(s string) ([]byte, error) {
	// RFC 9421 uses standard base64 with padding inside the byte-sequence
	// wrappers, but be lenient about missing padding.
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
