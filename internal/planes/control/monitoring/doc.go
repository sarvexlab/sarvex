// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package monitoring provides the continuous monitoring loop for Sarvex V1.
//
// The monitoring loop runs three concurrent tasks:
//
//  1. Semantic Probes — scheduled HTTP requests to the serving plane (as an agent would see them)
//     validating response structure, freshness, and business logic. Probes include:
//     - Profile endpoint (signature, UCP version, capabilities)
//     - Catalog search and lookup (response envelope, product fields)
//     - Checkout create/get/complete (session state machine, idempotency)
//     - Profile edge check (plugin's last-known-good fallback on Sarvex outage)
//
// 2. Consistency Engine — runs on every catalog change event, detecting divergence:
//   - Price drift (store ≠ served cache)
//   - Stock mismatch (in-stock claim without inventory)
//   - Missing required attributes (blocks UCP checkout)
//   - Compliance violations (spec violations in live profile)
//
// 3. Reconciliation Sweeper — scheduled full-catalog diff (hourly or on demand):
//   - Compares store ↔ catalog store for lost ingest signals
//   - Compares catalog store ↔ served cache for missed rebuilds
//   - Emits consistency findings if divergence detected
//   - Safety net under event-driven pipelines
//
// All findings are routed to the task service as alerts, routed by owner
// (merchant action, Sarvex internal, Google coordination).
package monitoring
