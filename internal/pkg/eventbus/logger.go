// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"github.com/ThreeDotsLabs/watermill"
)

// Logger is the logging interface accepted by EventBus.
// It mirrors watermill.LoggerAdapter so Watermill's own logger can be passed directly.
type Logger interface {
	watermill.LoggerAdapter
}

// noopLogger discards all log output.
type noopLogger struct{}

func (n *noopLogger) Error(msg string, err error, fields watermill.LogFields) {}
func (n *noopLogger) Info(msg string, fields watermill.LogFields)             {}
func (n *noopLogger) Debug(msg string, fields watermill.LogFields)            {}
func (n *noopLogger) Trace(msg string, fields watermill.LogFields)            {}
func (n *noopLogger) With(fields watermill.LogFields) watermill.LoggerAdapter { return n }

// StdLogger wraps watermill's standard logger for convenience.
func StdLogger(debug bool) Logger {
	return watermill.NewStdLogger(debug, debug)
}
