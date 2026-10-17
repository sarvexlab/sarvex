// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus"
	"github.com/sarvexlabs/sarvex.cp/internal/pkg/ucp/negotiation"
)

// WithRequestLogging captures request latency and outcome, emits to event bus.
func WithRequestLogging(pub eventbus.EventBus, log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			instance, ok := negotiation.GetStoreProfile(c)
			if !ok {
				return next(c)
			}

			err := next(c)

			latency := time.Since(start)
			logEv := &RequestLogEvent{
				MerchantID_: instance.Configuration().MerchantID,
				Handler:     c.Path(),
				Method:      c.Request().Method,
				Path:        c.Path(),
				LatencyMs:   latency.Milliseconds(),
				StatusCode:  c.Response().Status,
				RequestID:   c.Response().Header().Get(echo.HeaderXRequestID),
				OccurredAt_: start,
			}

			// Fire-and-forget emit
			EmitRequestLog(c.Request().Context(), pub, logEv, log)

			return err
		}
	}
}
