// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package checkoutescalation

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestNoImportPathToServingPlaneOrRedisSessionStore is A2: the projection
// worker must have no import path to serving-plane write code or Redis
// session mutation (R2 — this package is a read-only projection; it must
// never write back to Redis or influence checkout behavior).
//
// No import-boundary-testing convention exists elsewhere in this repo —
// the plane-isolation rule stated in internal/planes/doc.go is enforced by
// review, not tooling (.golangci.yml has no custom depguard rules). This
// is a new pattern, introduced here for this requirement specifically.
func TestNoImportPathToServingPlaneOrRedisSessionStore(t *testing.T) {
	forbidden := []string{
		"sarvex.cp/internal/planes/serving", // the plane this projection must never write to or call
		"github.com/redis/go-redis",         // the serving plane's session-store backend
	}

	out, err := exec.CommandContext(context.Background(), "go", "list", "-deps",
		"github.com/sarvexlabs/sarvex.cp/internal/planes/control/checkoutescalation").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	deps := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, dep := range deps {
		for _, f := range forbidden {
			if strings.Contains(dep, f) {
				t.Errorf("checkoutescalation must not import %s (found dependency: %s) — this package is a read-only projection and must never reach serving-plane write code or Redis session mutation (R2)", f, dep)
			}
		}
	}
}
