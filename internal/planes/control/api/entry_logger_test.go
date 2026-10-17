// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ThreeDotsLabs/watermill"
)

// countingHandler is a minimal slog.Handler test double that counts Handle
// calls and gates Enabled by a configured minimum level, so tests can assert
// eventBusLoggerAdapter never builds its args slice for a discarded record.
type countingHandler struct {
	minLevel slog.Level
	handled  int
}

func (h *countingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *countingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.handled++
	return nil
}

func (h *countingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(_ string) slog.Handler      { return h }

func TestEventBusLoggerAdapter_SkipsDisabledLevels(t *testing.T) {
	h := &countingHandler{minLevel: slog.LevelError}
	adapter := newEventBusLogger(slog.New(h))

	adapter.Info("info message", watermill.LogFields{"key": "value"})
	adapter.Debug("debug message", watermill.LogFields{"key": "value"})
	adapter.Trace("trace message", watermill.LogFields{"key": "value"})

	if h.handled != 0 {
		t.Errorf("expected 0 records handled for Info/Debug/Trace at level=Error, got %d", h.handled)
	}

	adapter.Error("error message", errors.New("boom"), watermill.LogFields{"key": "value"})
	if h.handled != 1 {
		t.Errorf("expected 1 record handled for the enabled Error call, got %d", h.handled)
	}
}

func TestEventBusLoggerAdapter_LogsEnabledLevels(t *testing.T) {
	h := &countingHandler{minLevel: slog.LevelDebug}
	adapter := newEventBusLogger(slog.New(h))

	adapter.Info("info message", watermill.LogFields{"key": "value"})
	adapter.Debug("debug message", watermill.LogFields{"key": "value"})
	adapter.Trace("trace message", watermill.LogFields{"key": "value"})
	adapter.Error("error message", errors.New("boom"), watermill.LogFields{"key": "value"})

	if h.handled != 4 {
		t.Errorf("expected 4 records handled with level=Debug, got %d", h.handled)
	}
}

func TestEventBusLoggerAdapter_With_ReturnsSameAdapter(t *testing.T) {
	adapter := newEventBusLogger(slog.New(&countingHandler{minLevel: slog.LevelDebug}))
	if got := adapter.With(watermill.LogFields{"key": "value"}); got != adapter {
		t.Error("expected With to return the same adapter (no per-call field binding implemented)")
	}
}
