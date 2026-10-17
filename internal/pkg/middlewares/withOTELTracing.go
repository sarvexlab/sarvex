// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"fmt"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const OTELTracerKey = "otel_tracer"

func WithOTELTracing(tracerProvider trace.TracerProvider, serviceName string) echo.MiddlewareFunc {
	tracerName := fmt.Sprintf("github.com/sarvexlabs/sarvex.cp/%s", serviceName)
	tracer := tracerProvider.Tracer(tracerName)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			opts := []trace.SpanStartOption{
				trace.WithAttributes(
					attribute.String("http.method", c.Request().Method),
					attribute.String("http.route", c.Path()),
					attribute.String("http.url", c.Request().URL.String()),
					attribute.String("http.client_ip", c.RealIP()),
					attribute.String("http.host", c.Request().Host),
					attribute.String("service.name", serviceName),
				),
			}

			// Get correlation ID if available
			correlationID := c.Get("X-Request-ID")
			if correlationID != nil {
				opts = append(opts, trace.WithAttributes(
					attribute.String("request.correlation_id", fmt.Sprintf("%v", correlationID)),
				))
			}

			spanName := fmt.Sprintf("%s %s", c.Request().Method, c.Path())
			ctx, span := tracer.Start(c.Request().Context(), spanName, opts...)
			defer span.End()

			// Store tracer in context for use by handlers
			c.Set(OTELTracerKey, tracer)

			// Update request context with trace context
			c.SetRequest(c.Request().WithContext(ctx))

			err := next(c)

			// Record response status
			span.SetAttributes(
				attribute.Int("http.status_code", c.Response().Status),
			)

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return err
			}

			if c.Response().Status >= 400 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", c.Response().Status))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return nil
		}
	}
}
