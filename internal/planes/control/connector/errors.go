// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package connector

import "errors"

var (
	ErrNotFound       = errors.New("connector: not found")
	ErrPairingExpired = errors.New("connector: pairing code expired")
)
