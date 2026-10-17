// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package archtest

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/catalog"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/sync"
)

// The sync plane writes remediation records (sync.Remediation) and the control
// plane reads them by collection name into its own read model
// (catalog.RemediationModel) — planes never share a type (CLAUDE.md §2.1), so
// this test is what holds the two shapes together.
func TestRemediationRecordContract(t *testing.T) {
	syncKeys := bsonKeys(reflect.TypeFor[sync.Remediation]())
	for key := range bsonKeys(reflect.TypeFor[catalog.RemediationModel]()) {
		if !syncKeys[key] {
			t.Errorf("control's RemediationModel reads %q, which the sync plane never writes", key)
		}
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	written := sync.Remediation{
		ID: "store-1:SKU-1:rule_x", MerchantID: "merch-1", StoreID: "store-1", SKU: "SKU-1",
		RuleID: "rule_x", RemediationRuleID: "rem_x", Field: "description",
		FromValue: "", ToValue: "fixed", Type: "rule", Trust: "high",
		Applied: true, WriteBackID: "wb-1", CreatedAt: now, UpdatedAt: now,
	}
	raw, err := bson.Marshal(written)
	if err != nil {
		t.Fatal(err)
	}
	var read catalog.RemediationModel
	if err := bson.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	want := catalog.RemediationModel{
		ID: written.ID, MerchantID: written.MerchantID, StoreID: written.StoreID, SKU: written.SKU,
		Field: written.Field, FromValue: written.FromValue, ToValue: written.ToValue,
		Type: written.Type, Trust: written.Trust, Applied: written.Applied, WriteBackID: written.WriteBackID,
		CreatedAt: now, UpdatedAt: now,
	}
	if !reflect.DeepEqual(read, want) {
		t.Errorf("control reads a sync-written record as\n%+v\nwant\n%+v", read, want)
	}
}

func bsonKeys(t reflect.Type) map[string]bool {
	keys := map[string]bool{}
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("bson")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}
