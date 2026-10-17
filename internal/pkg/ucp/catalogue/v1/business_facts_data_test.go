// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultRequiredBusinessFacts(t *testing.T) {
	facts := DefaultRequiredBusinessFacts()
	assert.NotEmpty(t, facts)

	seenIDs := make(map[string]bool)
	for _, f := range facts {
		assert.NotEmpty(t, f.ID)
		assert.False(t, seenIDs[f.ID], "duplicate required business fact ID: %s", f.ID)
		seenIDs[f.ID] = true
		assert.NotEmpty(t, f.Field)
		assert.Contains(t, []BusinessFactLevel{BusinessFactLevelMerchant, BusinessFactLevelStore}, f.Level)
	}

	// Both tenancy levels are represented (architecture §2a: merchant → store).
	var hasMerchant, hasStore bool
	for _, f := range facts {
		if f.Level == BusinessFactLevelMerchant {
			hasMerchant = true
		}
		if f.Level == BusinessFactLevelStore {
			hasStore = true
		}
	}
	assert.True(t, hasMerchant)
	assert.True(t, hasStore)
}

func TestDefaultCatalogueIncludesRequiredBusinessFacts(t *testing.T) {
	cat := DefaultCatalogue()
	assert.NotEmpty(t, cat.RequiredBusinessFacts)
}
