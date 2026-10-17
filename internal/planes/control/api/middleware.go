// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/account"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/auth"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/billing"
	"github.com/sarvexlabs/sarvex.cp/internal/planes/control/domain"
)

type contextKey string

const (
	ctxPrincipal contextKey = "principal"
)

// Type aliases for resolver functions (defined in auth package)
type (
	PrincipalResolver = auth.PrincipalResolver
	HMACResolver      = auth.HMACResolver
)

// requireAuth enforces bearer authentication and injects the Principal into the
// request context. merchant_id is never read from the URL — it comes from the
// resolved principal only.
//
// Member binding: if the principal resolver fails and the token contains an email
// claim, attempts to bind an invited member by email. On success, the invited member's
// merchant_id and role are used to create the principal.
//
// Operator elevation: when the resolved principal has the operator role and the
// request carries an X-Operate-As header, the header value becomes the principal's
// effective MerchantID for the duration of this request. This is the only way
// merchant_id enters the context for operator-scoped actions; the header value is
// audit-logged by the real operator service implementation (G6).
func requireAuth(resolve PrincipalResolver, accountSvc account.Service) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p, err := authenticate(c, bearerToken(c), resolve, accountSvc)
			if err != nil {
				return errUnauthorized(c)
			}
			c.Set(string(ctxPrincipal), p)
			return next(c)
		}
	}
}

// requireAuthWS is requireAuth's WebSocket-handshake counterpart. Browsers
// cannot set an Authorization header on a WebSocket upgrade request, so the
// bearer token instead travels as a Sec-WebSocket-Protocol entry (the
// standard workaround — see sarvex-v1-control-plane.yaml's
// /stores/{store_id}/journey/stream description). Everything past token
// extraction — resolution, invited-member binding, operator elevation — is
// identical to requireAuth.
func requireAuthWS(resolve PrincipalResolver, accountSvc account.Service) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p, err := authenticate(c, wsSubprotocolToken(c), resolve, accountSvc)
			if err != nil {
				return errUnauthorized(c)
			}
			c.Set(string(ctxPrincipal), p)
			return next(c)
		}
	}
}

// authenticate resolves token to a Principal, falling back to invited-member
// binding, and applies operator elevation via X-Operate-As. Shared by
// requireAuth and requireAuthWS so the two token-extraction paths (header vs.
// WebSocket subprotocol) can't drift in their resolution semantics.
func authenticate(c echo.Context, token string, resolve PrincipalResolver, accountSvc account.Service) (domain.Principal, error) {
	if token == "" {
		return domain.Principal{}, errors.New("no token")
	}
	p, err := resolve(token)
	if err != nil {
		// Try to bind an invited member by email.
		email, idpSub := extractEmailAndSubFromToken(token)
		if email != "" && idpSub != "" {
			member, merchantID, bindErr := accountSvc.BindInvitedMember(c.Request().Context(), email, idpSub)
			if bindErr == nil {
				// Binding succeeded; use the member's merchant_id and role.
				p = domain.Principal{
					UserID:     idpSub,
					Email:      email,
					Role:       member.Roles[0].ToRole(),
					MerchantID: merchantID,
				}
				err = nil
			}
		}
		if err != nil {
			return domain.Principal{}, err
		}
	}
	if p.Role == domain.RoleOperator {
		if operateAs := c.Request().Header.Get("X-Operate-As"); operateAs != "" {
			p.MerchantID = domain.MerchantID(operateAs)
		}
	}
	return p, nil
}

// requireOperator enforces that the caller has the operator role.
// It must be applied after requireAuth.
func requireOperator(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		p := principalFrom(c)
		if p.Role != domain.RoleOperator {
			return errForbidden(c)
		}
		return next(c)
	}
}

// requireOwner enforces that the caller has the merchant owner role. Used to gate
// self-serve actions (like inviting teammates) that only the account owner may take.
// It must be applied after requireAuth.
func requireOwner(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		p := principalFrom(c)
		if p.Role != domain.RoleOwner {
			return errForbidden(c)
		}
		return next(c)
	}
}

// requireActiveBilling blocks a merchant's console/API writes once its
// subscription has lapsed — trial ended without conversion, payment failed,
// or canceled (any status other than active/active_via_invoice). It must be
// applied after requireAuth. Per architecture invariant #2 this is strictly a
// control-plane concern: it is never wired into the serving plane, so a
// merchant's live UCP profile and checkout capability keep working
// regardless of billing status. Operators are never gated — an operator's
// own actions (including those elevated via X-Operate-As) are not limited by
// a merchant's billing status. billing/checkout-link and billing/portal-link
// are deliberately not wired to this middleware so a read-only merchant can
// still reactivate.
//
// billingReader is expected to be a billing.CachedPlanReader (see
// SetupRoutes) rather than billing.Service directly — this middleware runs
// on nearly every mutating request, so an uncached Get would round-trip
// Mongo once per request for a value that changes rarely. The parameter
// takes billing.PlanReader (just Get) rather than the full billing.Service
// so tests can stub it without satisfying the rest of the interface.
func requireActiveBilling(billingReader billing.PlanReader) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p := principalFrom(c)
			if p.Role == domain.RoleOperator {
				return next(c)
			}
			b, err := billingReader.Get(c.Request().Context(), p.MerchantID)
			if err != nil {
				return errInternal(c)
			}
			if b.Status != domain.BillingActive && b.Status != domain.BillingActiveViaInvoice {
				return errBillingReadOnly(c)
			}
			return next(c)
		}
	}
}

// requireAnalyticsTier gates the Lost Revenue Ledger's read API (R15) at
// the paid-analytics tier. There is no plan literally named "analytics" in
// this codebase's three commercial tiers (domain.PlanSelfServe,
// domain.PlanConcierge, domain.PlanScale — see web/console/src/lib/plans.ts
// for the merchant-facing names/pricing) — self_serve is the entry tier,
// concierge/scale are the two where Sarvex does hands-on work for the
// merchant. This reuses domain.Plan.IsConcierge() (billing.go), the
// existing "the two higher tiers" predicate already used to route task
// ownership (validation.Orchestrator.milestoneOwner,
// monitoring.TaskServiceRouter.RouteFindings) — bundling the ledger's read
// access with the same tier split rather than inventing a fourth,
// ledger-specific plan identifier.
//
// It must be applied after requireAuth. Operators are never gated, same
// rationale as requireActiveBilling. This governs reads only — R15 requires
// the worker to write for every tier regardless, so a merchant's ledger
// history is already complete the moment they upgrade into this tier.
func requireAnalyticsTier(billingReader billing.PlanReader) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			p := principalFrom(c)
			if p.Role == domain.RoleOperator {
				return next(c)
			}
			b, err := billingReader.Get(c.Request().Context(), p.MerchantID)
			if err != nil {
				return errInternal(c)
			}
			if !b.Plan.IsConcierge() {
				return errAnalyticsTierRequired(c)
			}
			return next(c)
		}
	}
}

// requireHMAC enforces HMAC-signed connector ingest requests per X-Sarvex-Signature header.
// Buffers the request body so it can be verified as part of the HMAC validation.
// The resolver validates signature over body + timestamp + sequence headers.
func requireHMAC(resolve HMACResolver) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sig := c.Request().Header.Get("X-Sarvex-Signature")
			if sig == "" {
				return errUnauthorized(c)
			}
			p, err := resolve(c, sig)
			if err != nil {
				return errUnauthorized(c)
			}
			c.Set(string(ctxPrincipal), p)
			return next(c)
		}
	}
}

// requireContactToken gates POST /contact with a static shared bearer token
// issued to the sarvex.example.com marketing website only — the website has no
// Sarvex account, so this is not requireAuth/Kinde. Follows the fail-closed,
// header-compared convention used by SIMULATION_SECRET
// (internal/planes/serving/api/simulation.go): an unconfigured token hides
// the route (404) rather than accepting unauthenticated submissions.
func requireContactToken(token string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if token == "" {
				return errNotFound(c)
			}
			if bearerToken(c) != token {
				return errUnauthorized(c)
			}
			return next(c)
		}
	}
}

// principalFrom extracts the resolved Principal from the context.
// Panics if called before requireAuth — that is a programming error.
func principalFrom(c echo.Context) domain.Principal {
	return c.Get(string(ctxPrincipal)).(domain.Principal)
}

func bearerToken(c echo.Context) string {
	h := c.Request().Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return after
	}
	return ""
}

// wsSubprotocolToken extracts the bearer token from a WebSocket handshake's
// Sec-WebSocket-Protocol header, e.g. "bearer, <token>" (the JS client passes
// this as the second element of the WebSocket constructor's protocols array,
// since it cannot set an Authorization header on the handshake request). The
// literal "bearer" tag itself is never returned as a token.
func wsSubprotocolToken(c echo.Context) string {
	h := c.Request().Header.Get("Sec-WebSocket-Protocol")
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if part != "" && part != "bearer" {
			return part
		}
	}
	return ""
}

// extractEmailAndSubFromToken extracts email and sub claims from a JWT without
// verification. Used for invited member binding fallback when JWT verification fails.
func extractEmailAndSubFromToken(token string) (email, sub string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", ""
	}

	if e, ok := claims["email"].(string); ok && e != "" {
		if s, ok := claims["sub"].(string); ok && s != "" {
			return e, s
		}
	}

	return "", ""
}
