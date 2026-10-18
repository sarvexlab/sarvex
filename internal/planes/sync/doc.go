// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package sync is the event-driven, eventually-consistent, retryable plane.
//
// Components here process catalog changes, run mapping and scoring, rebuild
// the serving cache, and hand off orders. All work is idempotent and
// partitioned per merchant. See sarvex-v1-architecture.md §4.1.
package sync
