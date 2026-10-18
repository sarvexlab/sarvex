// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package secretmanagement

import (
	"context"
	"testing"
)

func TestNew_RequiresVaultAddr(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected an error when VaultAddr is unset, got nil")
	}
}
