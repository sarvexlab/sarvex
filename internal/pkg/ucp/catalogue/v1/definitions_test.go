// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/wire"
)

func TestCatalogueStructure(t *testing.T) {
	cat := &Catalogue{
		Version:     "2026-06-15",
		SpecVersion: "2026-06-15",
	}

	assert.Equal(t, "2026-06-15", cat.Version)
	assert.Equal(t, "2026-06-15", cat.SpecVersion)
}

func TestCapabilityStructure(t *testing.T) {
	c := wire.Capability{
		ID:      "dev.ucp.shopping.checkout",
		Version: "1.0.0",
	}

	assert.Equal(t, "dev.ucp.shopping.checkout", c.ID)
}

func TestScoringRuleStructure(t *testing.T) {
	rule := ScoringRule{
		ID:             "rule_sku_missing",
		BlocksCheckout: true,
		Weight:         100,
	}

	assert.Equal(t, "rule_sku_missing", rule.ID)
	assert.True(t, rule.BlocksCheckout)
	assert.Equal(t, 100, rule.Weight)
}

func TestRemediationRuleStructure(t *testing.T) {
	rem := RemediationRule{
		ID:            "rem_sku_required",
		ScoringRuleID: "rule_sku_missing",
	}

	assert.Equal(t, "rem_sku_required", rem.ID)
	assert.Equal(t, "rule_sku_missing", rem.ScoringRuleID)
}
