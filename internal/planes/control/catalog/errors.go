// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package catalog

import "errors"

// ErrNotFound is returned when a requested catalog record does not exist.
var ErrNotFound = errors.New("catalog: not found")

// ErrRemediationApplied is returned when one or more remediations in a batch
// have already been applied. The entire batch is rejected — no partial applies.
var ErrRemediationApplied = errors.New("catalog: remediation already applied")

// ErrWriteBackNotFound is returned when a write-back record does not exist
// for the given merchant and write_back_id.
var ErrWriteBackNotFound = errors.New("catalog: write-back not found")
