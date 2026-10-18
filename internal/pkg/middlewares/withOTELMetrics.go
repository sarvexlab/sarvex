// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type httpMetrics struct {
	requestDuration metric.Float64Histogram
	requestCount    metric.Int64Counter
}

func WithOTELMetrics(mp metric.MeterProvider, serviceName string) echo.MiddlewareFunc {
	meterName := fmt.Sprintf("github.com/sarvexlab/sarvex/%s", serviceName)
	meter := mp.Meter(meterName)

	// Create instruments
	var metrics httpMetrics
	var err error

	metrics.requestDuration, err = meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request duration in seconds"),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create request duration histogram: %v", err))
	}

	metrics.requestCount, err = meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create request count counter: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			// Call handler
			err := next(c)

			// Record metrics
			duration := time.Since(start).Seconds()
			attrs := []attribute.KeyValue{
				attribute.String("http.method", c.Request().Method),
				attribute.String("http.route", c.Path()),
				attribute.Int("http.status_code", c.Response().Status),
				attribute.String("service.name", serviceName),
			}

			metrics.requestDuration.Record(c.Request().Context(), duration, metric.WithAttributes(attrs...))
			metrics.requestCount.Add(c.Request().Context(), 1, metric.WithAttributes(attrs...))

			return err
		}
	}
}
