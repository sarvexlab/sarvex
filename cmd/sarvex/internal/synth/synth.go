// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package synth derives a whole merchant fixture — account facts, stores,
// storefront URLs, contact details — from one thing the user actually
// typed: the merchant's name.
//
// Catalog contents are deliberately not generated here: the simulated
// store owns those (internal/planes/sync/sim CLI.Seed), and that is the
// catalog the real connector pulls from during import. A second generator
// would produce products nothing ever reads.
//
// Everything is deterministic. The RNG is seeded from a hash of the
// merchant name (or an explicit override), so the same name always yields
// the same fixture. That is what lets the conformance suite build its own
// world on every run instead of depending on a hand-seeded database, and
// what makes a failure reproducible from the run's seed alone.
//
// Nothing here is random-looking for its own sake: a reader comparing two
// runs should be able to tell instantly whether they used the same seed.
package synth

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"regexp"
	"strings"
)

// Merchant is everything derivable about a merchant from its name.
type Merchant struct {
	Name       string
	Slug       string
	OwnerEmail string
	// LegalEntityName, SupportEmail, SupportPhone and FulfillmentRegions
	// populate the account's business_facts.
	LegalEntityName    string
	SupportEmail       string
	SupportPhone       string
	FulfillmentRegions []string
}

// Store is everything derivable about one of a merchant's stores.
type Store struct {
	DisplayName     string
	Platform        string
	LegalName       string
	SupportEmail    string
	ReturnPolicyURL string
	Regions         []string
}

// Generator produces a merchant's fixture. Construct with New.
type Generator struct {
	seed int64
	rng  *rand.Rand
}

// New returns a Generator for the given merchant name. seed of 0 means
// "derive the seed from the name", which is the normal case; pass a
// non-zero seed to reproduce a specific earlier run.
func New(name string, seed int64) *Generator {
	if seed == 0 {
		seed = seedFromName(name)
	}
	// math/rand, deliberately: this generates test fixtures, and the whole
	// contract is that the same seed rebuilds the same merchant. A
	// cryptographic source would make failing runs irreproducible, which is
	// the opposite of what this is for. No fixture value is ever a secret.
	//nolint:gosec // G404: deterministic fixtures require a seedable PRNG
	return &Generator{seed: seed, rng: rand.New(rand.NewSource(seed))}
}

// Seed reports the seed in use, so a failing run can name it.
func (g *Generator) Seed() int64 { return g.seed }

func seedFromName(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	// Mask to 63 bits: rand.NewSource takes a signed seed, and a negative
	// one would make the printed seed awkward to pass back in.
	// The mask clears the sign bit, so the value always fits in a positive
	// int64 and the conversion cannot overflow.
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify reduces a display name to a URL- and identifier-safe token.
// Exported because store ids, hostnames and SKUs all key off it and callers
// occasionally need the same transform.
func Slugify(s string) string {
	slug := nonAlphanumeric.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "merchant"
	}
	return slug
}

// Merchant derives the merchant-level fixture.
//
// The domain is example.com rather than anything resolvable: these accounts
// are fictitious, and an address that looks deliverable invites someone to
// send to it. example.com is reserved by RFC 2606 for exactly this.
func (g *Generator) Merchant(name string) Merchant {
	slug := Slugify(name)
	return Merchant{
		Name:               name,
		Slug:               slug,
		OwnerEmail:         fmt.Sprintf("owner@%s.example.com", slug),
		LegalEntityName:    name + " Ltd",
		SupportEmail:       fmt.Sprintf("support@%s.example.com", slug),
		SupportPhone:       g.phone(),
		FulfillmentRegions: []string{"US", "CA"},
	}
}

// Store derives the fixture for a merchant's index-th store. Index 0 is the
// merchant's own name; later stores get a suffix, so a multi-store merchant
// reads as one business with several storefronts rather than as several
// unrelated businesses.
func (g *Generator) Store(m Merchant, index int, platform string) Store {
	name := m.Name
	if index > 0 {
		name = fmt.Sprintf("%s Store %d", m.Name, index+1)
	}
	slug := Slugify(name)
	return Store{
		DisplayName:     name,
		Platform:        platform,
		LegalName:       m.LegalEntityName,
		SupportEmail:    fmt.Sprintf("support@%s.example.com", slug),
		ReturnPolicyURL: fmt.Sprintf("https://%s.example.com/returns", slug),
		Regions:         m.FulfillmentRegions,
	}
}

func (g *Generator) phone() string {
	// +1-555-01xx is the North American fictitious-number range (555-0100
	// through 555-0199), reserved so it can never reach a real subscriber.
	return fmt.Sprintf("+1-555-01%02d", g.rng.Intn(100))
}
