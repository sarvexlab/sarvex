// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucp

import "fmt"

// StoreEndpointURL builds the serving-plane URL for a store's UCP API,
// mounted at /ucp/:platform/:storeID (see internal/planes/serving/api/entry.go's
// servingGroupName). Shared by every control-plane caller that addresses a
// specific store's serving endpoints (monitoring probes, pre-validation),
// so the routing convention lives in exactly one place.
func StoreEndpointURL(baseURL, platform, storeID string) string {
	return fmt.Sprintf("%s/ucp/%s/%s", baseURL, platform, storeID)
}
