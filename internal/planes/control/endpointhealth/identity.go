// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package endpointhealth implements synthetic probing, real-traffic
// evaluation, composite status, and alerting for merchant-facing UCP
// endpoints — the "Endpoint Health" system. It is a sibling to
// internal/planes/control/monitoring (which does consistency/freshness
// checking, a different concern) and lives in the control plane so it can
// reuse the store repository and the control plane's Mongo/Redis clients;
// it talks to the serving plane exclusively over HTTP, the same way
// monitoring.SemanticProbe already does, never by importing it.
package endpointhealth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"

	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/types"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpsig"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

// probeKeyID is fixed (not rotated) — the probe is a single, permanent
// Sarvex-held identity, not a per-store or per-merchant credential.
const probeKeyID = "sarvex-endpoint-health-probe"

// referenceVersion is the *protocol* version the probe presents, and it must
// be at least as new as the newest version Sarvex can serve.
//
// negotiation.SelectVersion picks the highest servable generation that
// does not exceed the platform's declared version, treating that version as
// a ceiling. A probe declaring an old version therefore matches nothing and
// is rejected outright with version_unsupported.
//
// This was deliberately ancient ("2024-01-01") while the gate was a single
// comparison — platform-newer-than-business fails — under which declaring an
// old version was what guaranteed acceptance. Version selection inverted that
// property exactly: the probe kept declaring 2024-01-01, negotiation_handshake
// then failed for every store, and activation could never leave
// profile_published.
//
// The registry's Latest rather than a pinned constant, because the property
// needed here is "at or above every servable generation" and Latest satisfies
// it by construction — the servable generations are a subset of the known ones,
// which Latest is
// computed from. It tracks new generations automatically and only ever moves
// forward, so this cannot drift out from under the servable set again.
var referenceVersion = wire.Version(specall.Default().Latest())

// referenceCapabilityVersion is the version the probe's *capabilities* carry,
// and it must be the version Sarvex actually serves.
//
// It is independent of referenceVersion and must stay so. The protocol
// version above is a ceiling, selected against; capability intersection is
// exact-match per capability, not newer-or-equal — negotiate.pickHighestMutual
// keeps a capability only when business and platform declare the identical
// version string. Following referenceVersion here would empty the intersection
// against every store that declares anything else, and the handshake would
// fail with "empty capability intersection" instead — the same dead end by a
// different route.
var referenceCapabilityVersion = wire.Version(specall.Default().Baseline())

// referenceHandlerNames are declared as supported by the probe's reference
// platform profile so negotiation_handshake finds a common payment handler
// regardless of which specific processor a given merchant has configured —
// a real platform (Google) would plausibly support all of these too.
var referenceHandlerNames = []string{"stripe", "shop_pay", "paypal", "adyen", "braintree", "google_pay"}

// Identity is the probe's fixed signing identity plus the reference
// "platform" profile it presents during the negotiation_handshake and
// signature_roundtrip probes.
type Identity struct {
	Signer     *ucpsig.Signer
	ProfileURL string
	Profile    wire.Profile
}

// Document is the reference profile as published: written in the form of
// the generation it declares.
func (i *Identity) Document() ([]byte, error) {
	gen, ok := specall.Default().Generations().Get(types.Version(i.Profile.UCP.Version))
	if !ok {
		return nil, fmt.Errorf("endpointhealth: no generation registered for %s", i.Profile.UCP.Version)
	}
	return gen.EncodeProfile(&i.Profile)
}

// identityDoc persists the probe's ECDSA key so every worker replica (and
// every restart) uses the same identity — merchants' serving-plane
// instances resolve this profile by URL and key id, so a rotating identity
// would invalidate itself constantly for no benefit.
type identityDoc struct {
	ID         string `bson:"_id"`
	PrivateKey string `bson:"private_key_d"` // base64url scalar
}

const identityDocID = "probe-identity"

// LoadOrCreateIdentity fetches the persisted probe identity, generating and
// storing one on first run. profileURL is this identity's own
// /.well-known/ucp URL (must be publicly reachable by every merchant's
// serving-plane instance for signature/negotiation verification to work).
func LoadOrCreateIdentity(ctx context.Context, db *mongo.Database, profileURL string) (*Identity, error) {
	col := db.Collection("control_endpoint_health_identity")

	var doc identityDoc
	err := col.FindOne(ctx, bson.M{"_id": identityDocID}).Decode(&doc)
	switch {
	case err == mongo.ErrNoDocuments:
		priv, genErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("generate probe identity key: %w", genErr)
		}
		candidate := identityDoc{
			ID:         identityDocID,
			PrivateKey: base64.RawURLEncoding.EncodeToString(priv.D.Bytes()),
		}
		if _, err := col.UpdateOne(ctx, bson.M{"_id": identityDocID}, bson.M{"$setOnInsert": candidate}, options.UpdateOne().SetUpsert(true)); err != nil {
			return nil, fmt.Errorf("persist probe identity: %w", err)
		}
		// Re-read: another replica may have won the upsert race with a
		// different key, and the stored key (not the one just generated
		// locally) must be what every replica signs with.
		if err := col.FindOne(ctx, bson.M{"_id": identityDocID}).Decode(&doc); err != nil {
			return nil, fmt.Errorf("read back probe identity: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("load probe identity: %w", err)
	}

	priv, err := privateKeyFromScalar(doc.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode probe identity key: %w", err)
	}

	jwk, err := ucpsig.JWKFromPrivateKey(probeKeyID, priv)
	if err != nil {
		return nil, fmt.Errorf("build probe identity JWK: %w", err)
	}
	signer, err := ucpsig.NewSigner(priv, jwk)
	if err != nil {
		return nil, fmt.Errorf("build probe identity signer: %w", err)
	}

	return &Identity{
		Signer:     signer,
		ProfileURL: profileURL,
		Profile:    referenceProfile(jwk),
	}, nil
}

// privateKeyFromScalar reconstructs an ECDSA P-256 private key from just
// its persisted scalar D by recomputing the public point via scalar base
// multiplication — the same relationship crypto/ecdsa.GenerateKey used to
// derive it in the first place.
func privateKeyFromScalar(scalarB64 string) (*ecdsa.PrivateKey, error) {
	dBytes, err := base64.RawURLEncoding.DecodeString(scalarB64)
	if err != nil {
		return nil, err
	}
	curve := elliptic.P256()
	d := new(big.Int).SetBytes(dBytes)
	x, y := curve.ScalarBaseMult(dBytes)
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}, nil
}

// referenceCapability is the probe's declaration of the named shopping
// capability, at referenceCapabilityVersion, with that generation's real
// ucp.dev spec and schema URLs.
func referenceCapability(name string) wire.Capability {
	v := referenceCapabilityVersion
	return wire.Capability{
		Version: v,
		Spec:    fmt.Sprintf("https://ucp.dev/%s/specification/%s", v, name),
		Schema:  fmt.Sprintf("https://ucp.dev/%s/schemas/shopping/%s.json", v, name),
	}
}

// referenceProfile is the canned "platform" profile the probe presents for
// negotiation_handshake — a checkout+catalog capability declaration using
// the real ucp.dev spec/schema URLs (required: NegotiationService.
// ValidateNamespaceBinding rejects a spec URL whose origin doesn't match
// the capability's reverse-domain prefix) plus every commonly-deployed
// payment handler name, so intersection succeeds against whatever a real
// merchant declares.
func referenceProfile(jwk ucpsig.JWK) wire.Profile {
	checkout := referenceCapability("checkout")
	catalog := referenceCapability("catalog")
	cart := referenceCapability("cart")

	handlers := make(map[string][]wire.PaymentHandler, len(referenceHandlerNames))
	for _, name := range referenceHandlerNames {
		handlers[name] = []wire.PaymentHandler{{ID: name, Version: referenceVersion}}
	}

	return wire.Profile{
		UCP: wire.UCP{
			Version: referenceVersion,
			Capabilities: map[string][]wire.Capability{
				"dev.ucp.shopping.checkout": {checkout},
				"dev.ucp.shopping.catalog":  {catalog},
				"dev.ucp.shopping.cart":     {cart},
			},
			PaymentHandlers: handlers,
		},
		Keys: []wire.JWK{
			{Kid: jwk.Kid, Kty: jwk.Kty, Crv: jwk.Crv, X: jwk.X, Y: jwk.Y, Use: jwk.Use, Alg: jwk.Alg},
		},
	}
}
