// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

// Package safego wraps goroutine launches so a panic inside them is
// recovered and logged instead of crashing the process. Reserve it for
// goroutines that run business logic against external or untrusted data
// (probe results, published events) and aren't already covered by a
// per-message or per-request recover layer (e.g. echo's middleware.Recover
// or watermill's middleware.Recoverer).
package safego

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine. A panic inside fn is recovered and logged
// via slog.Default() rather than propagating and taking down the process.
func Go(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recovered panic in goroutine",
					"goroutine", name,
					"panic", r,
					"stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
