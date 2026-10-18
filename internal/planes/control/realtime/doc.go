// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package realtime is the console-facing WebSocket fan-out for the control
// plane. It holds no state of its own truth — the journey state machine
// (internal/planes/control/journey) is the single writer — it only pushes a
// thin "something changed, re-fetch" signal to any open console session
// watching a store, so the console reflects journey transitions without
// polling.
//
// It exists as its own component (rather than living inside journey) because
// the eventbus is what makes it correct under horizontal scaling: a
// transition accepted by one control-plane instance publishes
// control.journey.updated, and every instance's Hub — including the one a
// given browser's WebSocket happens to be pinned to — receives it via the
// shared bus (NATS JetStream). A Hub with no
// eventbus subscription behind it would only ever see transitions accepted
// by its own instance.
package realtime
