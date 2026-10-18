// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package control is the interactive, human-facing control plane.
//
// It owns merchant accounts, the journey state machine, tasks, checklists,
// billing, notifications, and the operator board. All mutations are
// audit-logged and scoped to a single merchant resolved from the bearer token.
//
// merchant_id is NEVER a URL parameter — it is always resolved server-side
// from the authenticated principal. See sarvex-v1-architecture.md §4.5.
package control
