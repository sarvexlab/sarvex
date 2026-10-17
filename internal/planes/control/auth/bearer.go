// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	kindejwt "github.com/kinde-oss/kinde-go/jwt"

	"github.com/sarvexlab/sarvex/internal/planes/control/domain"
)

// KindeConfig holds the Kinde issuer and audience required for JWT verification.
type KindeConfig struct {
	// IssuerURL is the Kinde tenant URL, e.g. https://yourapp.kinde.com
	IssuerURL string
	// Audience is the Kinde API identifier registered for this backend.
	// Leave empty to skip audience validation.
	Audience string
}

// NewBearerResolver returns a PrincipalResolver that verifies Kinde JWT bearer tokens.
// The token parameter is the raw JWT string (Bearer prefix already stripped by middleware).
// Extracts merchant_id and role from JWT claims and returns a Principal.
func NewBearerResolver(log *slog.Logger, cfg KindeConfig) PrincipalResolver {
	jwksURL := cfg.IssuerURL + "/.well-known/jwks.json"

	opts := []func(*kindejwt.Token){
		kindejwt.WillValidateWithJWKSUrl(jwksURL),
		kindejwt.WillValidateAlgorithm("RS256"),
		kindejwt.WillValidateIssuer(cfg.IssuerURL),
	}
	if cfg.Audience != "" {
		opts = append(opts, kindejwt.WillValidateAudience(cfg.Audience))
	}

	return func(token string) (domain.Principal, error) {
		if token == "" {
			return domain.Principal{}, errors.New("empty bearer token")
		}

		parsed, err := kindejwt.ParseFromString(token, opts...)
		if err != nil || !parsed.IsValid() {
			if verr := parsed.GetValidationErrors(); verr != nil {
				return domain.Principal{}, errors.New("invalid bearer token: " + verr.Error())
			}
			if err != nil {
				return domain.Principal{}, errors.New("invalid bearer token: " + err.Error())
			}
			return domain.Principal{}, errors.New("invalid bearer token")
		}

		sub := parsed.GetSubject()
		if sub == "" {
			return domain.Principal{}, errors.New("sub claim missing in JWT")
		}

		merchantIDVal, ok := parsed.GetClaim("merchant_id")
		if !ok {
			return domain.Principal{}, errors.New("merchant_id claim missing in JWT")
		}
		merchantID, ok := merchantIDVal.(string)
		if !ok || merchantID == "" {
			return domain.Principal{}, errors.New("merchant_id claim missing in JWT")
		}

		role := resolveRole(parsed)
		if role == "" {
			return domain.Principal{}, errors.New("role claim missing in JWT")
		}

		email, _ := parsed.GetClaim("email")
		emailStr, _ := email.(string)

		return domain.Principal{
			UserID:     sub,
			Email:      emailStr,
			Role:       role,
			MerchantID: domain.MerchantID(merchantID),
		}, nil
	}
}

// NewSimBearerResolver returns a PrincipalResolver that trusts unsigned
// simulator JWTs (header.payload.anysig). The payload is a plain JSON object
// matching the claims the frontend simulator mints. Only use in development.
func NewSimBearerResolver() PrincipalResolver {
	return func(token string) (domain.Principal, error) {
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return domain.Principal{}, errors.New("invalid sim token")
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return domain.Principal{}, errors.New("invalid sim token payload: " + err.Error())
		}
		var claims struct {
			Sub        string   `json:"sub"`
			Email      string   `json:"email"`
			Role       string   `json:"role"`
			MerchantID string   `json:"merchant_id"`
			StoreIDs   []string `json:"store_ids"`
		}
		if err := json.Unmarshal(payload, &claims); err != nil {
			return domain.Principal{}, errors.New("invalid sim token payload: " + err.Error())
		}
		if claims.Sub == "" || claims.Role == "" {
			return domain.Principal{}, errors.New("sim token missing required claims")
		}
		// Operators have no merchant_id — that is valid.
		if claims.Role != "operator" && claims.MerchantID == "" {
			return domain.Principal{}, errors.New("sim token missing merchant_id for non-operator role")
		}
		return domain.Principal{
			UserID:     claims.Sub,
			Email:      claims.Email,
			Role:       domain.Role(claims.Role),
			MerchantID: domain.MerchantID(claims.MerchantID),
		}, nil
	}
}

// resolveRole extracts the principal's role from Kinde JWT claims.
// Kinde stores roles as an array of objects under "roles" with a "key" field.
// Falls back to a flat "role" string claim for flexibility.
func resolveRole(token *kindejwt.Token) domain.Role {
	if roles := token.GetRoles(); len(roles) > 0 {
		if roles[0].Key != "" {
			return domain.Role(roles[0].Key)
		}
	}
	if r, ok := token.GetClaim("role"); ok {
		if rs, ok := r.(string); ok && rs != "" {
			return domain.Role(rs)
		}
	}
	return ""
}
