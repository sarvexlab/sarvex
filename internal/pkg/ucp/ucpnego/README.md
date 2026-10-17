# ucpnego

A production-ready Go implementation of the [UCP negotiation protocol][spec] —
the capability handshake that runs on every UCP request between a platform
(agent) and a business (merchant).

[spec]: https://ucp.dev/latest/specification/overview/#negotiation-protocol

## Package layout

| Package | Concern | I/O? |
|---|---|---|
| `profile` | Wire types (`Profile`, `Capability`, `PaymentHandler`), version semantics, namespace authority validation | None |
| `negotiate` | Pure intersection algorithm + response-capability filtering | None |
| `httpx` | `UCP-Agent` parsing (RFC 8941), profile fetcher with bounded LRU cache, error/success response shapes, transport-neutral `BusinessNegotiator.Negotiate()`, net/http middleware, platform client | HTTP |
| `httpx/echox` | Echo v4 adapter (separate module — only pulled in if you import it) | HTTP |

The split keeps the algorithm trivially testable: `negotiate.Intersect` is a
pure function over two `*profile.Profile` values. All I/O lives in `httpx`.

## Where each piece is applicable

### Business side

You are a UCP business (merchant, marketplace) and need to:

- **Host your profile** at `/.well-known/ucp`. Use `profile.Profile` to build
  it, serve it with `Cache-Control: public, max-age >= 60`.
- **Resolve an incoming platform's profile** from the `UCP-Agent` header.
  Use `httpx.NewFetcher` (bounded LRU, HTTPS-only, no redirects) — it
  implements the discovery budget the spec calls out.
- **Run the intersection** on every UCP-bearing request. Use
  `httpx.NewBusinessNegotiator(...).Middleware(handler)` to do this
  declaratively; handlers read the result via `httpx.MustFromContext(ctx)`.
- **Emit the right response shape**: discovery/version failures use 4xx with
  a flat body; `capabilities_incompatible` uses 200 OK with
  `ucp.status = "error"`. The middleware handles both automatically.

### Platform side

You are a UCP platform (AI agent, commerce assistant) and need to:

- **Host your own profile** at any HTTPS URL. Same `profile.Profile` type.
- **Advertise it on every request** via the `UCP-Agent` header. Use
  `httpx.NewPlatformClient(profileURL)` — `client.Do(req)` injects the
  header automatically.
- **Parse the negotiated set** out of every response using
  `httpx.ResponseEnvelope` — treat `ucp.capabilities` in the response as
  authoritative (this is what the spec calls "server-selects").

### Both sides

- `profile.ValidateNamespaceBinding(capabilityName, specURL)` — platforms
  MUST run this on incoming capabilities; businesses SHOULD run it before
  publishing. Catches `dev.ucp.*` capabilities whose spec URL is hosted on
  an attacker domain.
- `negotiate.FilterResponseCapabilities(result, op)` — both sides need to
  know which capabilities are relevant to which operation type (checkout vs
  cart vs order).

## Quick start

### Business (Echo v4)

```go
import (
    "github.com/labstack/echo/v4"
    "github.com/sarvex/ucpnego/httpx"
    "github.com/sarvex/ucpnego/httpx/echox"
    "github.com/sarvex/ucpnego/negotiate"
)

own := buildMyProfile()
fetcher := httpx.NewFetcher(httpx.WithCacheSize(512))
neg, _ := httpx.NewBusinessNegotiator(own, fetcher,
    httpx.WithContinueURL(func(r *http.Request) string {
        return "https://merchant.example.com/cart"
    }),
)

e := echo.New()

// /.well-known/ucp MUST be served outside the negotiation middleware.
e.GET("/.well-known/ucp", echox.ProfileHandler(neg))

// Apply negotiation to a route group.
ucp := e.Group("/ucp/v1", echox.Middleware(neg))
ucp.POST("/checkout-sessions", createCheckout)

func createCheckout(c echo.Context) error {
    result := echox.MustResult(c)
    env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
    return c.JSON(http.StatusOK, map[string]any{
        "ucp":      env,
        "checkout": map[string]any{"id": "chk_123"},
    })
}
```

`echox` is a separate Go module (under `httpx/echox`), so the core `ucpnego`
module does not transitively pull in Echo. Only consumers who actually
import `httpx/echox` depend on `github.com/labstack/echo/v4`.

### Business (net/http)

```go
own := buildMyProfile() // *profile.Profile
fetcher := httpx.NewFetcher(httpx.WithCacheSize(512))
neg, _ := httpx.NewBusinessNegotiator(own, fetcher,
    httpx.WithContinueURL(func(r *http.Request) string {
        return "https://merchant.example.com/cart"
    }),
)

mux := http.NewServeMux()
mux.Handle("/.well-known/ucp", profileHandler(own))
mux.Handle("/checkout-sessions", neg.Middleware(http.HandlerFunc(createCheckout)))

func createCheckout(w http.ResponseWriter, r *http.Request) {
    result := httpx.MustFromContext(r.Context())
    env := httpx.BuildSuccessEnvelope(result, negotiate.OpCheckout)
    httpx.WriteSuccess(w, env, map[string]any{
        "checkout": map[string]any{"id": "chk_123", "status": "ready_for_complete"},
    })
}
```

### Platform (HTTP client)

```go
client, _ := httpx.NewPlatformClient("https://platform.example/.well-known/ucp")

req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
    "https://merchant.example.com/checkout-sessions", body)
req.Header.Set("Content-Type", "application/json")

resp, _ := client.Do(req) // UCP-Agent injected here

var env httpx.ResponseEnvelope
json.NewDecoder(resp.Body).Decode(&env)

if env.UCP.Status == "error" {
    // capabilities_incompatible — fall back to env.ContinueURL
    return
}
// env.UCP.Capabilities is the authoritative active set
```

### Running the examples

In one terminal:

```sh
go run ./examples/business
```

In another:

```sh
# the platform example serves its own profile on :9090 and POSTs to :8080
go run ./examples/platform -serve \
    -profile-url=http://localhost:9090/.well-known/ucp \
    -target=http://localhost:8080/checkout-sessions
```

(Use `http://` in the example only for localhost smoke-testing; the library
rejects non-HTTPS profile URLs in normal operation. The local examples bypass
this by serving the platform profile from a separate process.)

## Production notes

- **Cache backend.** `httpx.NewFetcher` ships with an in-process LRU. For
  multi-replica deployments swap it for a Redis-backed `ProfileSource` —
  it's a one-method interface.
- **Pre-approved registry.** The spec recommends fast-pathing known
  platforms. Wrap `ProfileSource` with a layer that consults your
  pre-approved registry first and falls back to dynamic discovery only for
  unknown profile URLs.
- **Async discovery.** For new platform URLs, return 503 + `Retry-After`,
  schedule the fetch in the background, then serve cached on retry. The
  current `BusinessNegotiator` does synchronous discovery; this is the
  natural place to plug in async behavior.
- **Observability.** Add an OpenTelemetry span around `Negotiate()` and
  emit metrics on intersection cardinality, discovery failures, and cache
  hit rate. The error path returns typed sentinels (`ErrProfileUnreachable`,
  etc.) so dashboards can break failures down by cause.
- **Signatures.** Negotiation does not require signatures, but the same
  `UCP-Agent` header is the identity binding for HTTP Message Signatures
  (RFC 9421). When you add signing, the keys come from
  `Profile.SigningKeys` — already parsed.
