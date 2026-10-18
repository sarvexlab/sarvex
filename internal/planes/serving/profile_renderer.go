// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package serving

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	specall "github.com/sarvexlab/sarvex/internal/spec/all"

	"github.com/sarvexlab/sarvex/internal/pkg/ucp"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/catalogue"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/paymenthandler"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/wire"
)

// ProfileRenderer builds spec-compliant UCP business profiles from store config using the catalogue.
// Per sarvex-v1-architecture.md: represents full UCP capability surface, implements checkout-first subset.
//
// NOTE: spec_url fields in the catalogue (services, capabilities, extensions, payment handlers, scoring rubric)
// are currently placeholder URLs (e.g., https://ucp.dev/published/2026-06-15/specification/).
// These are pending real spec hosting at ucp.dev and are documented in SPEC_RECONCILIATION.md.
// Agents consuming the profile may attempt to follow these URLs; they will 404 until spec hosting is live.
type ProfileRenderer struct {
	catalogue catalogue.Catalogue
	// servingOrigin is the serving plane's origin, without the /ucp prefix —
	// the base each rendered service endpoint is built from. Empty leaves the
	// catalogue's placeholder in place; see buildServices.
	servingOrigin string
	logger        *slog.Logger
}

// NewProfileRenderer creates a profile renderer. servingOrigin is the serving
// plane's own origin (no /ucp suffix — utils.Configurations.ServingOrigin),
// used to declare where this store's services actually answer.
func NewProfileRenderer(cat catalogue.Catalogue, servingOrigin string, logger *slog.Logger) *ProfileRenderer {
	return &ProfileRenderer{
		catalogue:     cat,
		servingOrigin: servingOrigin,
		logger:        logger,
	}
}

// RenderProfile builds a spec-compliant UCP profile from store configuration and catalogue.
// Returns a CacheProfile with all required fields per https://ucp.dev/latest/specification/overview/#business-profile
//
// The document's top-level ucp.version is the spec registry's Latest — the
// chronologically newest UCP generation Sarvex's protocol layer
// (negotiation decode/encode, discovery envelope) knows about, computed
// automatically as new generations are registered, not the version of
// every individual capability inside it. AI shopping agents prefer and
// search for the newest compatible version in a business's main manifest,
// so this is deliberately the ceiling, not the lowest common denominator:
// per the spec, each Capability entry carries its own version
// independently of ucp.version (Entity.Version — see
// buildCapabilities), so a capability whose response-building hasn't
// caught up with a newer generation (checkout may trail — see
// spec.Version.Serves) truthfully declares only the versions it can
// actually render, while the envelope itself and any capability that has
// caught up (catalog — see internal/spec/vYYYYMMDD/codec/catalog.go) advance to
// Latest. This is exactly the mechanism per-capability negotiation
// (IntersectCapabilities/wireintersect.Capabilities) already relies on to
// pick the highest *mutual* version per capability, not just per document.
func (pr *ProfileRenderer) RenderProfile(
	storeID string,
	merchantID string,
	profile *StoreProfileData,
) *CacheProfile {
	ucpVersion := specall.Default().Latest().String()

	// Build all spec-required components from catalogue
	services := pr.buildServices(storeID, profile.Platform, ucpVersion)
	capabilities := pr.buildCapabilities(profile)
	paymentHandlers := pr.buildPaymentHandlers(profile)

	cacheProfile := &CacheProfile{
		StoreID:       storeID,
		MerchantID:    merchantID,
		Platform:      profile.Platform,
		StorefrontURL: profile.StorefrontURL,
		UCP: UCPBlockCache{
			Version:         ucpVersion,
			Services:        services,
			Capabilities:    capabilities,
			PaymentHandlers: paymentHandlers,
		},
	}

	// Generations publish the keys under different names ("signing_keys",
	// then "keys"); the generation the document declares writes them, so a
	// document only ever carries its own generation's name.
	if err := pr.placeKeys(cacheProfile, profile.SigningKeys); err != nil {
		pr.logger.Error("render profile keys", "store_id", storeID, "version", ucpVersion, "error", err)
	}

	return cacheProfile
}

// placeKeys writes keys into cp under the member name of the generation cp
// declares, by having that generation encode them.
func (pr *ProfileRenderer) placeKeys(cp *CacheProfile, keys []wire.JWK) error {
	if len(keys) == 0 {
		return nil
	}
	gen, ok := specall.Default().Generations().Get(types.Version(cp.UCP.Version))
	if !ok {
		return fmt.Errorf("no generation registered for %s", cp.UCP.Version)
	}
	doc, err := gen.EncodeProfile(&wire.Profile{UCP: wire.UCP{Version: wire.Version(cp.UCP.Version)}, Keys: keys})
	if err != nil {
		return err
	}
	var placed struct {
		SigningKeys []wire.JWK `json:"signing_keys"`
		Keys        []wire.JWK `json:"keys"`
	}
	if err := json.Unmarshal(doc, &placed); err != nil {
		return err
	}
	cp.SigningKeys = ConvertJWKsToSigningKeyCache(placed.SigningKeys)
	cp.Keys = ConvertJWKsToSigningKeyCache(placed.Keys)
	return nil
}

// buildServices builds the services map from the catalogue.
//
// The catalogue ships a placeholder endpoint (the service id — see
// catalogue_data.go), because the catalogue is store-agnostic and cannot know
// where any given store is served. Rendering it through unchanged declares
// "endpoint": "dev.ucp.shopping" in the published profile, which is not a URL:
// an agent has nowhere to go, and the profile_reachable probe fails with an
// endpoint mismatch, pinning the store at profile_published forever.
// buildServices builds the services map from the catalogue, stamping each
// service with the endpoint this store actually answers on and the
// envelope generation this rendering declares (ucpVersion — RenderProfile's
// the registry's Latest, the same value ucp.version carries at the document's top
// level).
//
// A service entry's Version is the transport BINDING's own spec generation
// (which rest.openapi.json/mcp contract it conforms to — Service's own doc
// comment), an envelope-level concept that tracks the document overall, not
// a per-capability rendering promise: "can Sarvex actually render checkout
// at 2026-08-25" is answered separately and correctly by
// capabilityVersionEntries (what each generation serves decides, regardless of
// what this stamps). The catalogue's own static Version on each service
// (DefaultCatalogue, pinned to 2026-04-08 same as everything else in that
// hardcoded, checkout-first-authored file) is therefore overridden here,
// the same way ucpVersion already overrides it for the document's own
// top-level version — leaving the two to drift is what let this render a
// profile whose service binding claimed an older generation than the
// document it was published in.
func (pr *ProfileRenderer) buildServices(storeID, platform string, ucpVersion string) map[string][]wire.Service {
	result := make(map[string][]wire.Service)

	if pr.catalogue == nil {
		pr.logger.Warn("catalogue not available, returning empty services")
		return result
	}

	catalogueServices := pr.catalogue.Services()
	if len(catalogueServices) == 0 {
		pr.logger.Debug("no services in catalogue")
		return result
	}

	// svc is a copy per iteration, so overwriting Endpoint/Version here never
	// mutates the shared catalogue.
	for _, svc := range catalogueServices {
		if pr.servingOrigin != "" {
			svc.Endpoint = ucp.StoreEndpointURL(pr.servingOrigin, platform, storeID)
		}
		svc.Version = wire.Version(ucpVersion)
		svc.Spec = atSpecVersion(svc.Spec, pr.catalogue.SpecVersion(), ucpVersion)
		svc.Schema = atSpecVersion(svc.Schema, pr.catalogue.SpecVersion(), ucpVersion)
		result[svc.ID] = []wire.Service{svc}
	}

	pr.logger.Debug("built services from catalogue", "service_count", len(result))
	return result
}

// buildCapabilities builds the capabilities map from catalogue (V1-implemented only).
// Only includes capabilities marked v1_implemented in the catalogue.
//
// Each capability's own Entity.Version is independent of the document's
// top-level ucp.version (see RenderProfile's doc comment) — a business
// declares a capability at every version it can genuinely render, per
// capabilityVersionEntries, not at whatever ucp.version happens to be.
func (pr *ProfileRenderer) buildCapabilities(profile *StoreProfileData) map[string][]wire.Capability {
	result := make(map[string][]wire.Capability)

	if pr.catalogue == nil {
		pr.logger.Warn("catalogue not available, returning empty capabilities")
		return result
	}

	v1Caps := pr.catalogue.CapabilitiesForV1()
	if len(v1Caps) == 0 {
		pr.logger.Debug("no V1 capabilities in catalogue")
		return result
	}

	// Use catalogue capabilities directly - already spec-compliant
	for _, catCap := range v1Caps {
		result[catCap.ID] = capabilityVersionEntries(catCap, pr.catalogue.SpecVersion())
	}

	// A store's own capability set narrows which V1 capabilities it declares;
	// it never replaces them. Replacing would swap the per-version entries
	// (a business declares a capability at every version it can render — see
	// capabilityVersionEntries) for whatever single entry the stored set
	// carries, and would let a stale or hand-edited set declare capabilities
	// the V1 gate excludes.
	if len(profile.Capabilities) > 0 {
		for id := range result {
			if _, enabled := profile.Capabilities[id]; !enabled {
				delete(result, id)
			}
		}
	}

	dropOrphanedExtensions(result)

	pr.logger.Debug("built capabilities from catalogue", "capability_count", len(result))
	return result
}

// dropOrphanedExtensions removes every extension none of whose parents is
// declared. Per the UCP spec an extension's `extends` names capabilities the
// business must itself advertise, so an extension that outlives its parent
// (parent disabled for the store) is not reachable and must not be published.
// Removal cascades: an extension of a removed extension goes too.
func dropOrphanedExtensions(caps map[string][]wire.Capability) {
	for changed := true; changed; {
		changed = false
		for id, entries := range caps {
			if len(entries) == 0 || !entries[0].IsExtension() {
				continue
			}
			hasParent := false
			for _, parent := range entries[0].ExtendsList() {
				if _, ok := caps[parent]; ok {
					hasParent = true
					break
				}
			}
			if !hasParent {
				delete(caps, id)
				changed = true
			}
		}
	}
}

// capabilityVersionEntries returns one Capability entry per generation base
// can actually be rendered in: the generations whose module serves it
// (spec.Version.Serves — each one's codec renders it).
//
// Each entry's spec and schema URLs are the ones for ITS version: the
// catalogue is authored at 2026-04-08, and an entry declared at 2026-08-25
// that still pointed at 2026-04-08's schema would send an agent to the wrong
// contract for the version it negotiated.
//
// This is what makes the floating registry Latest (RenderProfile's
// ucp.version) automatically safe as more generations get registered:
// declaring a version here means Sarvex will actually render a response
// at it, and that's driven by what each generation serves — a deliberate,
// reviewed decision in its own module — never by Latest alone. Declaring more than a capability
// can render would let a platform negotiate a response it will never get
// (sarvex-v1-architecture.md §D R3).
func capabilityVersionEntries(base wire.Capability, authoredAt string) []wire.Capability {
	versions := specall.Default().Serving(base.ID)

	entries := make([]wire.Capability, len(versions))
	for i, v := range versions {
		entry := base
		entry.Version = wire.Version(v)
		entry.Spec = atSpecVersion(entry.Spec, authoredAt, v.String())
		entry.Schema = atSpecVersion(entry.Schema, authoredAt, v.String())
		entries[i] = entry
	}
	return entries
}

// buildPaymentHandlers builds the payment_handlers block for a store.
//
// Candidates are the catalogue handlers that can process payments on the
// store's platform (Shop Pay is Shopify-only). When the merchant has saved a
// payment configuration, only handlers enabled in it are published, each with
// the public `config` an agent needs to tokenize against it; an enabled handler
// whose parameters are incomplete is withheld rather than published unusable.
// A store with no payment configuration at all keeps the legacy behaviour —
// every applicable handler, without config — so stores that predate per-store
// payment settings keep transacting until their merchant configures them.
func (pr *ProfileRenderer) buildPaymentHandlers(profile *StoreProfileData) map[string][]wire.PaymentHandler {
	result := make(map[string][]wire.PaymentHandler)

	if pr.catalogue == nil {
		pr.logger.Warn("catalogue not available, returning empty payment handlers")
		return result
	}

	for _, handler := range pr.catalogue.PaymentHandlersForPlatform(profile.Platform) {
		if profile.PaymentHandlers != nil {
			setting, ok := findPaymentHandlerSetting(profile.PaymentHandlers, handler.ID)
			if !ok || !setting.Enabled {
				continue
			}
			spec, known := paymenthandler.Lookup(handler.ID)
			if !known {
				continue
			}
			cfg, err := spec.ProfileConfig(setting.Params)
			if err != nil {
				pr.logger.Warn("payment handler enabled but not fully configured, not publishing",
					"handler", handler.ID, "store_id", profile.StoreID, "error", err)
				continue
			}
			if len(cfg) > 0 {
				raw, err := json.Marshal(cfg)
				if err != nil {
					pr.logger.Warn("payment handler config cannot be encoded, not publishing",
						"handler", handler.ID, "store_id", profile.StoreID, "error", err)
					continue
				}
				handler.Config = raw
			}
		}
		result[handler.ID] = []wire.PaymentHandler{handler}
	}

	pr.logger.Debug("built payment handlers", "handler_count", len(result))
	return result
}

// findPaymentHandlerSetting looks a handler up by name, tolerating a setting
// stored under the handler's short id.
func findPaymentHandlerSetting(settings map[string]PaymentHandlerSetting, name string) (PaymentHandlerSetting, bool) {
	if s, ok := settings[name]; ok {
		return s, true
	}
	if spec, ok := paymenthandler.Lookup(name); ok {
		if s, ok := settings[spec.ID]; ok {
			return s, true
		}
	}
	return PaymentHandlerSetting{}, false
}

// atSpecVersion rewrites a catalogue URL of the form
// https://ucp.dev/<authoredAt>/... — authoredAt being the generation the
// catalogue's spec and schema URLs are written at (Catalogue.SpecVersion) — to
// the given generation. A URL that does not carry the catalogue's generation
// (a third-party handler's, say) is returned unchanged.
func atSpecVersion(url, authoredAt, version string) string {
	return strings.Replace(url, "/"+authoredAt+"/", "/"+version+"/", 1)
}
