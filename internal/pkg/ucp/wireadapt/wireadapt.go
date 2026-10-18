// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

// Package wireadapt translates Sarvex's canonical model to and from each served
// UCP spec generation's wire form: checkout, cart and order, and catalog reads.
//
// Internally the money path (session store, connectors, order handoff) speaks
// one canonical model, internal/pkg/commerce/model. Each generation is served
// by translating at the HTTP edge, rather than forking the money path per
// generation (sarvex-v1-architecture.md §D R1/R3).
//
// Each generation is one Codec, in its own package (spec/v20260408/codec,
// spec/v20260825/codec, ...), registered in a Registry. Adding a generation
// means adding a module and one line in internal/spec/all; no established
// generation's code is touched, so none needs re-verifying beyond the shared
// per-version conformance matrix.
package wireadapt

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sarvexlab/sarvex/internal/pkg/commerce/model"
	"github.com/sarvexlab/sarvex/internal/pkg/ucp/types"
)

// Resource names the kind of UCP resource being translated.
type Resource string

const (
	Checkout Resource = "checkout"
	Cart     Resource = "cart"
	Order    Resource = "order"
)

// Capabilities lists, in name order, the capabilities a response of resource
// r declares in its "ucp" envelope: the resource's own capability and the
// extensions it carries. It is the same on every platform and in every
// generation — only the declared version differs, and that is each codec's.
func (r Resource) Capabilities() []string {
	switch r {
	case Checkout:
		return []string{"dev.ucp.shopping.buyer_consent", "dev.ucp.shopping.checkout", "dev.ucp.shopping.discount", "dev.ucp.shopping.fulfillment"}
	case Cart:
		return []string{"dev.ucp.shopping.cart", "dev.ucp.shopping.discount"}
	case Order:
		return []string{"dev.ucp.shopping.order"}
	}
	return nil
}

// WithEnvelope sets body's "ucp" response metadata to generation v's for
// resource r: the version, and each of the resource's capabilities at that
// version. The canonical model carries no envelope — the envelope is the
// generation's, so each codec stamps its own. (The negotiation middleware
// later replaces it with the negotiated result where there was one.)
func WithEnvelope(body []byte, v types.Version, r Resource) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, fmt.Errorf("wireadapt: %s is not a JSON object: %w", r, err)
	}
	caps := map[string][]map[string]string{}
	for _, c := range r.Capabilities() {
		caps[c] = []map[string]string{{"version": string(v)}}
	}
	env, err := json.Marshal(map[string]any{"version": string(v), "capabilities": caps})
	if err != nil {
		return nil, err
	}
	obj["ucp"] = env
	return json.Marshal(obj)
}

// RequestExtras is information a generation's request carries that the
// canonical model cannot represent, kept so the response can echo it.
type RequestExtras struct {
	// BuyerConsent is the request's buyer.consent exactly as sent, in the
	// generation's own shape (nil when the request carried none).
	BuyerConsent json.RawMessage
}

// Codec is one spec generation's contract for the resources Sarvex serves: the
// translation between its wire form and the canonical model, for checkout,
// cart and order, and its rendering of catalog reads.
type Codec interface {
	// Version is the generation this codec speaks.
	Version() types.Version

	// DecodeRequest converts a request body of this generation to the canonical
	// body the handlers bind, and returns what the canonical form cannot hold.
	DecodeRequest(body []byte) ([]byte, RequestExtras, error)

	// EncodeResponse renders a canonical response (its JSON) of resource r in
	// this generation's wire form. consentWire returns the request's original
	// buyer.consent for the checkout being answered; a generation whose
	// consent carries more than the canonical form calls it to keep that
	// detail, and one that does not never pays for the lookup.
	EncodeResponse(r Resource, canonical []byte, consentWire func() json.RawMessage) ([]byte, error)

	// DecodeCatalogSearch reads a catalog search_request of this generation.
	DecodeCatalogSearch(body []byte) (model.CatalogQuery, error)

	// EncodeCatalogSearch renders a page of search results as this
	// generation's search_response.
	EncodeCatalogSearch(page model.CatalogPage) ([]byte, error)

	// EncodeCatalogProduct renders one product as this generation's Product.
	EncodeCatalogProduct(p model.CatalogProduct) ([]byte, error)
}

// Registry holds the Codecs of the generations Sarvex serves.
type Registry struct {
	codecs map[types.Version]Codec
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{codecs: map[types.Version]Codec{}} }

// Register adds c. It panics on a duplicate version: two codecs claiming one
// generation is a startup-time programming error.
func (r *Registry) Register(c Codec) {
	v := c.Version()
	if _, dup := r.codecs[v]; dup {
		panic(fmt.Sprintf("wireadapt: codec for %s already registered", v))
	}
	r.codecs[v] = c
}

// Supported reports whether responses can be rendered in generation v.
func (r *Registry) Supported(v types.Version) bool {
	_, ok := r.codecs[v]
	return ok
}

// Versions lists the registered generations, oldest first.
func (r *Registry) Versions() []types.Version {
	out := make([]types.Version, 0, len(r.codecs))
	for v := range r.codecs {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Baseline is the oldest registered generation: what a request that was never
// negotiated (it carried no UCP-Agent) is served in.
func (r *Registry) Baseline() types.Version {
	vs := r.Versions()
	if len(vs) == 0 {
		panic("wireadapt: no codec registered")
	}
	return vs[0]
}

// codec returns the codec for v, or an error naming the unsupported version.
func (r *Registry) codec(v types.Version) (Codec, error) {
	c, ok := r.codecs[v]
	if !ok {
		return nil, fmt.Errorf("wireadapt: no codec for spec version %s", v)
	}
	return c, nil
}

// DecodeRequest converts a request body of generation v to the canonical body.
func (r *Registry) DecodeRequest(v types.Version, body []byte) ([]byte, RequestExtras, error) {
	c, err := r.codec(v)
	if err != nil {
		return nil, RequestExtras{}, err
	}
	return c.DecodeRequest(body)
}

// EncodeResponse renders a canonical response (any value that marshals to the
// canonical JSON of the given resource) in generation v's wire form.
func (r *Registry) EncodeResponse(v types.Version, res Resource, canonical any, consentWire func() json.RawMessage) ([]byte, error) {
	c, err := r.codec(v)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("wireadapt: marshal canonical %s: %w", res, err)
	}
	return c.EncodeResponse(res, raw, consentWire)
}

// DecodeCatalogSearch reads a catalog search_request of generation v.
func (r *Registry) DecodeCatalogSearch(v types.Version, body []byte) (model.CatalogQuery, error) {
	c, err := r.codec(v)
	if err != nil {
		return model.CatalogQuery{}, err
	}
	return c.DecodeCatalogSearch(body)
}

// EncodeCatalogSearch renders a page of search results in generation v.
func (r *Registry) EncodeCatalogSearch(v types.Version, page model.CatalogPage) ([]byte, error) {
	c, err := r.codec(v)
	if err != nil {
		return nil, err
	}
	return c.EncodeCatalogSearch(page)
}

// EncodeCatalogProduct renders one product in generation v.
func (r *Registry) EncodeCatalogProduct(v types.Version, p model.CatalogProduct) ([]byte, error) {
	c, err := r.codec(v)
	if err != nil {
		return nil, err
	}
	return c.EncodeCatalogProduct(p)
}
