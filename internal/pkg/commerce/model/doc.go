// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package model is Sarvex's canonical commerce model: the checkout, cart,
// order and catalog shapes the connectors produce and the checkout money path
// (session store, idempotency, order handoff) works in. It belongs to no spec
// generation and no platform — each served generation renders it in its own
// wire form (internal/pkg/ucp/wireadapt), and each platform's connector maps
// its store onto it (docs/plans/version-platform-modularity.md, Phase 2).
//
// The types are Sarvex's own. They started as a copy of the 2026-04-08 wire
// types the money path uses, so their JSON is still byte-identical to that
// generation's (which is why the 2026-04-08 codec only adds an envelope);
// from here they evolve with Sarvex, and each generation's codec owns the
// difference to its wire form. Nothing outside a version's own packages
// reaches these shapes through internal/spec/vYYYYMMDD.
package model
