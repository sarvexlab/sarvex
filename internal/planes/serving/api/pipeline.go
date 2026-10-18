// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/ucpsig"
	"github.com/sarvexlab/sarvex/internal/planes/serving"
)

// RequestPipeline builds the middleware stack for serving-plane requests.
type RequestPipeline struct {
	middlewares []echo.MiddlewareFunc
}

func NewRequestPipeline() *RequestPipeline {
	return &RequestPipeline{
		middlewares: []echo.MiddlewareFunc{},
	}
}

// WithIdentity adds instance provider middleware.
func (p *RequestPipeline) WithIdentity(provider ucp.StoreProviderFunc) *RequestPipeline {
	p.middlewares = append(p.middlewares, negotiation.WithStoreConfigProvider(provider))
	return p
}

// WithSignatureVerification adds signature verification middleware.
func (p *RequestPipeline) WithSignatureVerification(config ucpsig.SignatureConfig) *RequestPipeline {
	p.middlewares = append(p.middlewares, ucpsig.WithRequestSignatureVerifier(config))
	return p
}

// WithNegotiation adds negotiation middleware.
func (p *RequestPipeline) WithNegotiation(svc *negotiation.NegotiationService, pub eventbus.EventBus) *RequestPipeline {
	p.middlewares = append(p.middlewares, negotiation.WithNegotiation(svc, pub))
	return p
}

// WithRateLimiting adds rate limiting middleware.
func (p *RequestPipeline) WithRateLimiting(limiter serving.RateLimiter) *RequestPipeline {
	p.middlewares = append(p.middlewares, serving.WithRateLimiting(limiter))
	return p
}

// WithRequestLogging adds request logging middleware.
func (p *RequestPipeline) WithRequestLogging(pub eventbus.EventBus, log *slog.Logger) *RequestPipeline {
	p.middlewares = append(p.middlewares, serving.WithRequestLogging(pub, log))
	return p
}

// WithTelemetry adds the endpoint-health telemetry middleware. Must be
// added first (outermost) so it observes signature/negotiation failures
// that short-circuit the rest of the chain.
func (p *RequestPipeline) WithTelemetry(pub eventbus.EventBus, log *slog.Logger) *RequestPipeline {
	p.middlewares = append(p.middlewares, serving.WithTelemetry(pub, log))
	return p
}

// WithResponseSigning adds response signing middleware.
func (p *RequestPipeline) WithResponseSigning(config ucpsig.SignatureConfig) *RequestPipeline {
	p.middlewares = append(p.middlewares, ucpsig.WithResponseSignatureGenerator(config))
	return p
}

// Build returns the complete middleware stack.
func (p *RequestPipeline) Build() []echo.MiddlewareFunc {
	return p.middlewares
}
