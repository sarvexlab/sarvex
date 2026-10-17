// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	specall "github.com/sarvexlabs/sarvex.cp/internal/spec/all"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/types"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/ucpsig"
)

// The reference profile is published in the form of the generation it
// declares, so that generation's own reader finds its key. (It used to be
// written with 2026-04-08's "signing_keys" while declaring specall.Default().Latest().)
func TestIdentityDocument_IsInTheDeclaredGenerationsForm(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwk, err := ucpsig.JWKFromPrivateKey(probeKeyID, priv)
	if err != nil {
		t.Fatal(err)
	}
	id := &Identity{Profile: referenceProfile(jwk)}

	doc, err := id.Document()
	if err != nil {
		t.Fatal(err)
	}
	gen, ok := specall.Default().Generations().Get(types.Version(id.Profile.UCP.Version))
	if !ok {
		t.Fatalf("no generation for declared version %s", id.Profile.UCP.Version)
	}
	back, err := gen.DecodeProfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	if back.FindKey(probeKeyID) == nil {
		t.Errorf("%s's reader does not find the probe key in %s", gen.Version(), doc)
	}
}
