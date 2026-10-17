// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package planes contains the three architectural planes of Sarvex V1.
//
// Each sub-package owns exactly one plane and must never import from another:
//
//   - control  — interactive, authenticated humans, iterates daily
//   - sync     — event-driven, eventually consistent, retryable
//   - serving  — always-on, low-latency, agent-facing
//
// The plane boundaries are the most important invariant in the system.
// See sarvex-v1-architecture.md §4.1.
package planes
