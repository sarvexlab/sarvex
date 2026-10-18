// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package account

import "errors"

// ErrNotFound is returned when a requested account or member record does not exist.
var ErrNotFound = errors.New("account: not found")

// ErrMemberAlreadyActive is returned when attempting to invite a member who is already active.
var ErrMemberAlreadyActive = errors.New("account: member already active")

// ErrMemberLimitReached is returned when a self-serve invite would exceed the
// merchant's team-member seat cap.
var ErrMemberLimitReached = errors.New("account: member limit reached")
