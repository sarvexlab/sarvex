// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package negotiation

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sarvexlab/sarvex/internal/pkg/eventbus"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
)

// isCheckoutRequest reports whether r targets a checkout endpoint —
// "/checkout-sessions" or "/checkout-sessions/{id}[/complete]" — the one
// capability domain NegotiateCheckout's narrower servable set applies to.
// V1 has exactly one checkout surface (checkoutSurface in the serving
// plane's checkout_contract.go), so a path substring is enough; if a
// second surface is ever added under a different path, this is the one
// place that needs to know about it.
func isCheckoutRequest(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/checkout-sessions")
}

func WithStoreConfigProvider(provider ucp.StoreProviderFunc) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Implementation for instance provider middleware
			storeID := c.Param("storeID")
			// Thread the request context through provider to respect cancellation (F-030)
			storeProvider, err := provider(c.Request().Context(), storeID)
			if errors.Is(err, ucp.ErrStoreProfileNotFound) {
				// The store exists but has no cached snapshot yet (paired but
				// not yet imported, or an unknown store_id) — a 404, not a
				// server failure. Discovery/read paths never call live
				// (sarvex-v1-architecture.md A10), so this is the expected shape of
				// "nothing to show yet."
				return echo.NewHTTPError(http.StatusNotFound, "store not found")
			}
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to get store provider")
			}
			SetStoreProfile(c, storeProvider)
			return next(c)
		}
	}
}

// WithNegotiation returns an Echo middleware that performs UCP capability
// negotiation for every request that carries a UCP-Agent header.
// Requests without the header are passed through unchanged (non-UCP requests).
//
// Prerequisites:
//   - A tenant-resolution middleware must run before this middleware and place a
//     UCPInstanceProvider into the Echo context under UCPInstanceKey.
//
// publisher may be nil; if non-nil, negotiation events are published after
// each successful negotiation.
func WithNegotiation(svc *NegotiationService, publisher ...eventbus.EventBus) echo.MiddlewareFunc {
	var bus eventbus.EventBus
	if len(publisher) > 0 {
		bus = publisher[0]
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 1. Extract the platform profile URI from the UCP-Agent header.
			// Absent header means this is a non-UCP request — pass through.
			profileURI, err := svc.ParsePlatformProfileURI(c.Request())
			if err != nil {
				return c.JSON(http.StatusBadRequest, ucp.NegotiationErrorResponse{
					Code:    "invalid_profile_url",
					Content: err.Error(),
				})
			}
			if profileURI == "" {
				return next(c)
			}

			// 2. Retrieve the UCPInstance placed by the upstream tenant middleware.
			inst, ok := GetStoreProfile(c)
			if !ok {
				return c.JSON(http.StatusInternalServerError, ucp.UCPErrorResponse{
					Status: "error",
					Messages: []ucp.UCPMsg{{
						Type:     "error",
						Code:     "missing_ucp_instance",
						Content:  "merchant middleware did not populate the UCP instance",
						Severity: "requires_buyer_input",
					}},
				})
			}

			// 3. Capture continue_url from the instance (for error responses).
			continueURL := inst.StorefrontURL()

			// 4. Run negotiation. Checkout is restricted to
			// Config.CheckoutServableVersions (see NegotiateCheckout's doc
			// comment) — everything else (catalog reads today) follows
			// Config.ServableVersions.
			negotiate := svc.Negotiate
			if isCheckoutRequest(c.Request()) {
				negotiate = svc.NegotiateCheckout
			}
			headerVersion := svc.ParseHeaderVersion(c.Request())
			result, platformProfile, events, err := negotiate(c.Request().Context(), inst, profileURI, headerVersion)
			svc.PublishNegotiationEvents(c.Request().Context(), bus, events)
			if err != nil {
				SetOutcome(c, "failed")
				return negotiationErrorResponse(c, err, continueURL)
			}

			// 5. Store platform profile in context for downstream signature
			// validation — only when one was actually fetched (negotiate's
			// isDereferenceableURL fallback returns a nil platformProfile;
			// setting a nil *wire.Profile here would make GetPlatformProfile's
			// ok-check true with a nil value instead of correctly reporting
			// "no platform profile" via ok=false).
			if platformProfile != nil {
				c.Set(PlatformProfileKey, platformProfile)
			}

			// 6. Store result in Echo context.
			SetNegotiationResult(c, result)
			SetOutcome(c, "succeeded")

			// 7. Wrap the response writer with a buffering recorder.
			original := c.Response().Writer
			rec := &responseRecorder{original: original, statusCode: http.StatusOK}
			c.Response().Writer = rec

			// 8. Call the next handler.
			handlerErr := next(c)

			// 9. Restore the original writer unconditionally so any subsequent
			// middleware or Echo's own error handler uses the real connection.
			c.Response().Writer = original

			// 10. Only flush the buffered body if the handler actually committed a
			// response (i.e. called WriteHeader/Write). If the handler returned an
			// error without writing anything, c.Response().Committed is false and
			// we leave the wire untouched so Echo's error handler can respond.
			if c.Response().Committed {
				injected, injectErr := injectUCPResponse(rec.body.Bytes(), result)
				if injectErr != nil {
					injected = rec.body.Bytes()
				}
				original.Header().Set("Content-Length", strconv.Itoa(len(injected)))
				original.WriteHeader(rec.statusCode)
				_, _ = original.Write(injected)
			}

			return handlerErr
		}
	}
}

// negotiationErrorResponse maps typed negotiation errors to spec-compliant HTTP responses.
//
// Error-to-status mapping per spec:
//   - invalid_profile_url       → 400  (malformed UCP-Agent header)
//   - profile_unreachable       → 424  (fetch failed: timeout or non-2xx)
//   - profile_malformed         → 422  (invalid JSON or namespace binding failure)
//   - version_unsupported       → 422  (platform version newer than business)
//   - capabilities_incompatible → 200  (empty intersection; ucp.status="error")
func negotiationErrorResponse(c echo.Context, err error, continueURL string) error {
	var (
		unreachable  *ucp.ProfileUnreachableError
		malformed    *ucp.ProfileMalformedError
		versionErr   *ucp.VersionUnsupportedError
		incompatible *ucp.CapabilitiesIncompatibleError
	)
	switch {
	case errors.As(err, &unreachable):
		return c.JSON(http.StatusFailedDependency, ucp.NegotiationErrorResponse{
			Code:        "profile_unreachable",
			Content:     err.Error(),
			ContinueURL: continueURL,
		})
	case errors.As(err, &malformed):
		return c.JSON(http.StatusUnprocessableEntity, ucp.NegotiationErrorResponse{
			Code:        "profile_malformed",
			Content:     err.Error(),
			ContinueURL: continueURL,
		})
	case errors.As(err, &versionErr):
		return c.JSON(http.StatusUnprocessableEntity, ucp.NegotiationErrorResponse{
			Code:        "version_unsupported",
			Content:     err.Error(),
			ContinueURL: continueURL,
		})
	case errors.As(err, &incompatible):
		return c.JSON(http.StatusOK, ucp.CapabilitiesIncompatibleResponse{
			UCP: ucp.CapabilitiesErrorUCPBlock{
				Version:      incompatible.BusinessVersion,
				Status:       "error",
				Capabilities: map[string]any{},
			},
			Messages: []ucp.UCPMsg{{
				Type:     "error",
				Code:     "capabilities_incompatible",
				Content:  "no compatible capabilities in intersection",
				Severity: "unrecoverable",
			}},
			ContinueURL: continueURL,
		})
	default:
		return c.JSON(http.StatusBadGateway, ucp.NegotiationErrorResponse{
			Code:        "negotiation_failed",
			Content:     err.Error(),
			ContinueURL: continueURL,
		})
	}
}

// responseRecorder is an http.ResponseWriter that buffers the response body
// while forwarding header mutations to the original writer.
type responseRecorder struct {
	original   http.ResponseWriter
	body       bytes.Buffer
	statusCode int
}

// Header forwards to the original writer so that Content-Type and other headers
// set by the handler are available on the actual response.
func (r *responseRecorder) Header() http.Header {
	return r.original.Header()
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

// injectUCPResponse merges the ucp field into a JSON object body.
// If the body is not a JSON object, it is returned unchanged.
func injectUCPResponse(body []byte, result *NegotiationResult) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return body, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return body, nil
	}

	ucpJSON, err := json.Marshal(result)
	if err != nil {
		return body, err
	}
	obj["ucp"] = ucpJSON

	return json.Marshal(obj)
}
