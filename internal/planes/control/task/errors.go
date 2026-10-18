// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package task

import "errors"

var ErrNotFound = errors.New("task: not found")
var ErrNotResolvable = errors.New("task: not in a resolvable state")

// ErrAlreadyExists reports that a task with this id was inserted by someone
// else between the caller's existence check and its insert. Every raiser of
// a prescribed or deduped task (task.Ensure, the sync.task.raised
// subscriber, the monitoring loop) does check-then-insert on the same
// deterministic id, so this race is expected under concurrent reconciliation
// passes, not exceptional — callers treat it the same as finding the task
// already there.
var ErrAlreadyExists = errors.New("task: already exists")

// ErrInvalidCursor reports a page cursor the service did not issue. Surfaced
// as a 400 rather than silently restarting from the first page, so a client
// paginating with a stale or malformed cursor learns it is not seeing the
// slice of the feed it asked for.
var ErrInvalidCursor = errors.New("task: invalid cursor")
