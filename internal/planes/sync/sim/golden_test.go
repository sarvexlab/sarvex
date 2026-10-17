// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim_test

import (
	"path"
	"testing"

	"github.com/sarvexlab/sarvex/internal/pkg/golden"
)

// The capability matrix freezes what every (platform, spec generation) pair
// produces today, in testdata/golden/<platform>/<version>/<step>.json, plus the
// connector's own generation-neutral output in <platform>/canonical/. Adding a
// platform or a generation adds goldens; it cannot change an existing one
// without the change showing up in review (see internal/pkg/golden).

// goldenRun is what identifies and normalises one matrix subtest's goldens.
type goldenRun struct {
	platform, serverURL, storeID string
}

// canonicalGolden is the golden "version" directory for connector output,
// which is generation-neutral.
const canonicalGolden = "canonical"

// assertGolden checks one step's output, masking the test server's address
// and the per-subtest store id.
func assertGolden(t *testing.T, g goldenRun, version, step string, got any) {
	t.Helper()
	golden.Assert(t, path.Join(g.platform, version, step), got, map[string]string{
		g.serverURL: "http://sim.test",
		g.storeID:   "store-matrix",
	})
}
