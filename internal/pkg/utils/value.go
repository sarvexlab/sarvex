// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package utils

// StringValue returns the value of the string pointer passed in or
// "" if the pointer is nil.
func StringValue(v *string) string {
	if v != nil {
		return *v
	}
	return ""
}
