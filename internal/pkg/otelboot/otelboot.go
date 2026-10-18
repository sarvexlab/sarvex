// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package otelboot wires the OpenTelemetry SDK into the global otel
// providers when an OTLP collector endpoint is configured, leaving the
// default no-op providers untouched otherwise. Every plane's
// middlewares.WithOTELTracing/WithOTELMetrics call (and every
// otel.Tracer(...) call in the UCP connector clients) already reads from the
// global providers — this package is the only place that decides what those
// providers actually are.
package otelboot

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// Setup registers a real OTLP-exporting TracerProvider and MeterProvider as
// the global otel providers when OTEL_EXPORTER_OTLP_ENDPOINT is set (e.g. by
// scripts/dev/up.sh, pointed at the local collector in
// deployments/docker-compose.yaml). Both exporters read the standard
// OTEL_EXPORTER_OTLP_* env vars themselves, so no endpoint parsing happens
// here.
//
// When the env var is unset — any environment without a collector, such as
// CI or an unconfigured deployment — Setup does nothing, so callers keep
// getting the default no-op providers exactly as before this package
// existed.
//
// The returned shutdown func flushes and closes whatever was set up; it is
// always safe to call and a no-op when nothing was configured.
func Setup(ctx context.Context, serviceName string) func(context.Context) error {
	noop := func(context.Context) error { return nil }

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
	)
	if err != nil {
		res = resource.Default()
	}

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return noop
	}

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return func(shutdownCtx context.Context) error { return traceExporter.Shutdown(shutdownCtx) }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func(shutdownCtx context.Context) error {
		var errs []error
		if err := tp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("tracer provider shutdown: %w", err))
		}
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("meter provider shutdown: %w", err))
		}
		return errors.Join(errs...)
	}
}
