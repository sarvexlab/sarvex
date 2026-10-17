// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package ucp

import "testing"

func TestStoreEndpointURL(t *testing.T) {
	got := StoreEndpointURL("https://serving.example.com", "woocommerce", "store-42")
	want := "https://serving.example.com/ucp/woocommerce/store-42"
	if got != want {
		t.Errorf("StoreEndpointURL() = %q, want %q", got, want)
	}
}

func TestStoreEndpointURL_DifferentStoresDifferentURLs(t *testing.T) {
	a := StoreEndpointURL("https://serving.example.com", "woocommerce", "store-a")
	b := StoreEndpointURL("https://serving.example.com", "woocommerce", "store-b")
	if a == b {
		t.Errorf("expected different stores to produce different URLs, both were %q", a)
	}
}
