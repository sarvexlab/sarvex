// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/platform"
)

// Each platform's store is paired with that platform's own simulated
// credentials. The simulator used to send WooCommerce credentials for every
// store, so a Magento store paired in dev mode got a config its connector
// could not use.
func TestPairingCredentialsFollowTheStoresPlatform(t *testing.T) {
	for _, p := range platform.All() {
		t.Run(string(p.Name), func(t *testing.T) {
			got, err := pairingCredentials("http://sim.test", "store-1", p.Name)
			require.NoError(t, err)
			assert.Equal(t, p.Sim.Credentials("http://sim.test", "store-1"), got)
		})
	}

	woo, err := pairingCredentials("http://sim.test", "store-1", "woocommerce")
	require.NoError(t, err)
	mg, err := pairingCredentials("http://sim.test", "store-1", "magento")
	require.NoError(t, err)
	assert.Contains(t, woo, "consumer_key")
	assert.Contains(t, mg, "admin_user")
	assert.NotContains(t, mg, "consumer_key")

	_, err = pairingCredentials("http://sim.test", "store-1", "shopify")
	assert.Error(t, err, "a platform with no simulated store cannot be paired")
}
