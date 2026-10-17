// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package endpointhealth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/platform/woocommerce"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sarvexlab/sarvex/internal/pkg/domain"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/negotiation"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// The rest of this package's probe tests drive the runner against
// negotiatingMux, a fixture that fabricates the serving plane's negotiation
// response. That is the right shape for asserting how the probe *reports*
// each outcome, but it means nothing in the suite ever ran the probe's
// reference profile past the real negotiation engine — and a regression
// slipped through exactly there.
//
// referenceVersion used to be "2024-01-01", chosen because the old gate was
// a single comparison (platform-newer-than-business fails) under which an
// old version was always accepted. negotiation.SelectVersion replaced that
// with "highest servable version not exceeding the platform's", inverting
// the property: 2024-01-01 then matched nothing in specall.Default().Servable(), every
// store failed negotiation_handshake with version_unsupported, and
// activation could never leave profile_published.
//
// These tests close that gap by wiring the production negotiation service —
// negotiation.ServableVersions(), the same value
// serving/api/dependencies.go passes — to the probe's real profile.

// negotiatedStore stands in for a Sarvex store as the negotiation engine
// sees it, declaring what serving's ProfileRenderer actually renders:
// catalog at every specall.Default().Servable() version (it has response-building for
// each), checkout and cart at specall.Default().Serving("dev.ucp.shopping.checkout") only, and the
// catalogue's two payment handlers. Deliberately built from those package
// values rather than from referenceCapabilityVersion, so that a change to
// the probe's declared capability version that stops matching real stores
// fails here instead of shipping.
type negotiatedStore struct{}

func (negotiatedStore) StoreID() string { return "store_negotiation_fixture" }

func (negotiatedStore) Version() string { return specall.Default().Latest().String() }

func (negotiatedStore) Services() map[string][]wire.Service {
	return map[string][]wire.Service{
		string(domain.UCPServiceTypeShopping): {{
			Endpoint: "https://sarvex.test/ucp/store_negotiation_fixture",
			Version:  wire.Version(types.Version("2026-04-08")),
		}},
	}
}

func (negotiatedStore) Capabilities() map[string][]wire.Capability {
	caps := map[string][]wire.Capability{}
	for _, id := range []domain.CapabilityType{
		domain.CapabilityTypeCatalog,
		domain.CapabilityTypeCheckout,
		domain.CapabilityTypeCart,
	} {
		versions := specall.Default().Serving("dev.ucp.shopping.checkout")
		if id == domain.CapabilityTypeCatalog {
			versions = specall.Default().Servable()
		}
		entries := make([]wire.Capability, 0, len(versions))
		for _, v := range versions {
			entries = append(entries, wire.Capability{
				Version: wire.Version(v),
				Spec:    "https://ucp.dev/" + v.String() + "/specification/x",
				Schema:  "https://ucp.dev/" + v.String() + "/schemas/shopping/x.json",
			})
		}
		caps[string(id)] = entries
	}
	return caps
}

func (negotiatedStore) PaymentHandlers() map[string][]wire.PaymentHandler {
	return map[string][]wire.PaymentHandler{
		"google_pay": {{ID: "google_pay", Version: wire.Version(types.Version("2026-04-08"))}},
		"shop_pay":   {{ID: "shop_pay", Version: wire.Version(types.Version("2026-04-08"))}},
	}
}

func (negotiatedStore) SigningKeys() []wire.JWK { return nil }

func (negotiatedStore) StorefrontURL() string { return "https://fixture.example.com" }

func (negotiatedStore) Configuration() ucp.Configuration {
	return ucp.Configuration{
		MerchantID: "merchant_negotiation_fixture",
		StoreID:    "store_negotiation_fixture",
		Platform:   woocommerce.PlatformName,
	}
}

// productionNegotiationService builds the negotiation service with exactly
// the servable sets serving/api/dependencies.go wires in production. Any
// test that pinned its own narrower set here would stop testing the thing
// that broke.
func productionNegotiationService() *negotiation.NegotiationService {
	return negotiation.NewNegotiationService(
		negotiation.WithServableVersions(negotiation.ServableVersions()),
		negotiation.WithCheckoutServableVersions(negotiation.CheckoutServableVersions()),
	)
}

// serveReferenceProfile publishes the probe's own reference profile at a
// URL, the way the probe's UCP-Agent header points the serving plane at it.
func serveReferenceProfile(t *testing.T, profile wire.Profile) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(profile))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/.well-known/ucp"
}

// The regression, pinned at its narrowest: the version the probe declares
// must be selectable from the production servable set. This is the single
// assertion whose absence let negotiation_handshake fail for every store.
func TestReferenceVersionIsSelectableFromServableVersions(t *testing.T) {
	chosen, ok := negotiation.SelectVersion(negotiation.ServableVersions(), referenceVersion)
	require.True(t, ok,
		"the probe declares %q, which selects nothing from specall.Default().Servable() %v — "+
			"every store's negotiation_handshake fails with version_unsupported and "+
			"activation stalls at profile_published", referenceVersion, specall.Default().Servable())
	assert.Equal(t, wire.Version(specall.Default().Latest()), chosen,
		"expected the newest servable version to be selected")
}

// The same property through the whole engine: the probe's real profile,
// fetched over HTTP as the serving plane fetches it, negotiated against a
// store shaped like a real one.
func TestReferenceProfileNegotiatesAgainstProductionService(t *testing.T) {
	identity := testIdentity(t)
	profileURL := serveReferenceProfile(t, identity.Profile)

	result, _, _, err := productionNegotiationService().Negotiate(t.Context(), negotiatedStore{}, profileURL)
	require.NoError(t, err, "the probe's reference profile must negotiate against a real store")
	require.NotNil(t, result)

	assert.Equal(t, specall.Default().Latest().String(), result.Version)

	// A non-empty capability intersection is what the probe actually
	// checks (probeSignatureAndNegotiation treats an empty one as
	// FailureNoCommonCapability), and catalog is the capability
	// probeCatalogLookup goes on to use.
	require.NotEmpty(t, result.Capabilities, "empty capability intersection")
	assert.Contains(t, result.Capabilities, string(domain.CapabilityTypeCatalog))

	// referenceHandlerNames exists so the handshake finds a common
	// handler; assert it does rather than trusting the list stays in step
	// with the catalogue's ids.
	assert.NotEmpty(t, result.PaymentHandlers, "no common payment handler with a real store")
}

// Checkout negotiates from its own servable set (specall.Default().Serving("dev.ucp.shopping.checkout")), which
// today reaches the newest generation like catalog does. A probe at that
// generation must select it, so the money path is exercised in the version a
// current agent will actually use.
func TestReferenceProfileNegotiatesCheckoutAgainstProductionService(t *testing.T) {
	identity := testIdentity(t)
	profileURL := serveReferenceProfile(t, identity.Profile)

	result, _, _, err := productionNegotiationService().NegotiateCheckout(t.Context(), negotiatedStore{}, profileURL)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, specall.Default().Latest().String(), result.Version,
		"checkout must negotiate at the newest generation it serves")
}
