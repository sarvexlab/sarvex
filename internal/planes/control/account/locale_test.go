// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package account

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticLocaleProvider_List(t *testing.T) {
	provider := NewStaticLocaleProvider()
	countries, err := provider.List(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, countries)

	seen := make(map[string]bool)
	for _, c := range countries {
		assert.NotEmpty(t, c.Code, "every country must have a code")
		assert.NotEmpty(t, c.Name, "every country must have a name")
		assert.NotEmpty(t, c.TimeZones, "country %s must have at least one timezone", c.Code)
		assert.False(t, seen[c.Code], "duplicate country code %s", c.Code)
		seen[c.Code] = true
	}
	assert.True(t, seen["AU"], "expected AU in the static locale list")
}
