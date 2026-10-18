// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package simstore is the platform-neutral part of the dev-mode store
// simulator: the simulated store model (Store, Product, Order), the Surface
// each platform module implements to serve it in that platform's wire shape
// (internal/platform/<name>/sim), and the conventions both dev-mode plugin
// simulators (internal/planes/control/sim and internal/planes/sync/sim) share
// so they agree on a simulated store's base URL and credentials without any
// cross-process or cross-plane coordination: every value derives from
// store_id alone.
//
// Never used in production — both simulators that import this package are
// gated on KINDE_ISSUER_URL being unset (dev/test mode only).
package simstore

import "fmt"

// DefaultBaseURL is used when SIM_STORE_BASE_URL is unset, so the control
// plane (issuing credentials) and the sync plane (serving the fake store
// those credentials point at) agree on a base URL without either needing
// explicit configuration in the common local-dev case.
const DefaultBaseURL = "http://localhost:4106"

// StoreBaseURL returns the base URL a simulated store is reachable at, on
// every platform, given the sim store server's own base URL
// (SIM_STORE_BASE_URL) and the store_id path segment it serves under.
func StoreBaseURL(simServerBaseURL, storeID string) string {
	return fmt.Sprintf("%s/stores/%s", simServerBaseURL, storeID)
}

// WebhookSecret deterministically derives the webhook secret placeholder the
// store_credentials_* wire schemas require. The sim store
// never verifies webhook signatures, so this only needs to satisfy the
// schema's required field, not back a real check.
func WebhookSecret(storeID string) string {
	return "wh_sim_" + storeID
}
