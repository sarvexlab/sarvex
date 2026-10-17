// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestNoImportPathToServingPlane enforces the plane-isolation invariant
// (CLAUDE.md §2.1, internal/planes/doc.go) for this package: it consumes
// serving-plane event *payloads* over the bus (JSON), and must never
// import the serving plane's Go code to get them — see model.go's doc
// comment on why Surface is redeclared here rather than imported.
// Mirrors internal/planes/control/channel/isolation_test.go's identical
// check for that package.
func TestNoImportPathToServingPlane(t *testing.T) {
	out, err := exec.CommandContext(context.Background(), "go", "list", "-deps",
		"github.com/sarvexlabs/sarvex.cp/internal/planes/control/insights").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	deps := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, dep := range deps {
		if strings.Contains(dep, "sarvex.cp/internal/planes/serving") || strings.Contains(dep, "sarvex.cp/internal/planes/sync") {
			t.Errorf("insights must not import %s — this package must stay decoupled from the serving/sync planes' Go code, communicating only over the event bus", dep)
		}
	}
}
