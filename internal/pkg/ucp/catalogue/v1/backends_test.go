// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlabs/sarvex.cp/internal/platform"
)

// The catalogue publishes one backend composition per registered platform,
// straight from the registry — it used to list WooCommerce only, so Magento
// never appeared (e.g. in the gateway-cli wizard's backend choice).
func TestCompositionsCoverEveryRegisteredPlatform(t *testing.T) {
	comps := NewCommerceBackendServiceRegistry().Compositions
	require.Len(t, comps, len(platform.All()))
	for _, p := range platform.All() {
		c, ok := comps[p.Name]
		if assert.True(t, ok, "no composition for %s", p.Name) {
			assert.Equal(t, p.Name, c.BackendType)
			assert.JSONEq(t, string(p.ConfigSchema), string(c.ConfigSchema))
		}
	}
}
