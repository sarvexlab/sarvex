// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package serving is the always-on, low-latency, agent-facing plane.
//
// Invariant: this plane reads only the serving cache for discovery/read paths.
// Checkout operations call the connector synchronously for real-time stock,
// shipping rates, and tax. See sarvex-v1-architecture.md §4.1, §4.5.
package serving
