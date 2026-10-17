# ucpsig

A production-grade Go implementation of [UCP HTTP Message Signatures](https://ucp.dev/latest/specification/signatures/) (RFC 9421 over the UCP profile, with RFC 9530 Content-Digest and JWK keys).

## What's in the box

| File | Responsibility |
|---|---|
| `doc.go` | Package docs + UCP error registry (`signature_invalid`, `key_not_found`, …) |
| `jwk.go` | JWK ↔ `ecdsa` conversions for P-256/P-384 |
| `digest.go` | RFC 9530 `Content-Digest` compute + verify (sha-256) |
| `base.go` | RFC 9421 signature base builder, `Signature-Input` parser, `UCP-Agent` parser |
| `ecdsa.go` | ECDSA sign/verify with **raw `r‖s`** encoding (not DER — UCP requirement) |
| `profile.go` | `/.well-known/ucp` fetcher with TTL cache, in-flight dedup, trust allow-list |
| `signer.go` | High-level `Signer` — request and response signing |
| `verifier.go` | High-level `Verifier` — request and response verification |
| `middleware.go` | `SigningTransport`, `VerifyMiddleware` (including buffered response signing) |
| `ucpsig_test.go` | Round-trip, tamper-detection, wrong-key, end-to-end tests |
| `example/platform/` | Runnable platform-side service |
| `example/business/` | Runnable business-side service |

## Applicability — who signs what, and who verifies what

The spec splits responsibility along two axes: which party is acting (Platform / Business), and whether the message is a request or a response. The package supplies a primitive for each cell:

| Scenario | Outbound side uses | Inbound side uses | Required? |
|---|---|---|---|
| **Platform → Business request** (e.g. `POST /checkout-sessions`) | `SigningTransport` | `VerifyMiddleware` | SHOULD per spec |
| **Business → Platform response** (e.g. `201 Created` with checkout) | `VerifyMiddleware`'s response-signing path | `Verifier.VerifyResponse` | RECOMMENDED for payment auth + checkout completion |
| **Business → Platform webhook** (server-initiated push) | `SigningTransport` | `VerifyMiddleware` | **MUST** per spec |
| **Platform → Business MCP call** (JSON-RPC over HTTP) | `SigningTransport` | `VerifyMiddleware` | Same as REST — uniform binding |

There is **no MCP-specific code** because the spec deliberately uses streamable HTTP for MCP, so the same RFC 9421 plumbing covers both transports. The body just happens to contain JSON-RPC instead of REST JSON — `Content-Digest` covers raw bytes either way.

## Platform side (signing outbound, verifying inbound webhooks)

```go
priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
jwk, _  := ucpsig.JWKFromPublicKey("platform-2026-01", &priv.PublicKey)
signer, _ := ucpsig.NewSigner(priv, jwk)

// 1. Outbound: every call to a business is signed.
client := &http.Client{Transport: &ucpsig.SigningTransport{
    Signer:     signer,
    ProfileURL: "https://platform.example/.well-known/ucp",
    Base:       http.DefaultTransport,
}}

// 2. Publish your public key.
mux.HandleFunc("/.well-known/ucp", func(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(ucpsig.Profile{SigningKeys: []ucpsig.JWK{jwk}})
})

// 3. Inbound webhooks: verify.
resolver := ucpsig.NewHTTPProfileResolver(nil, 5*time.Minute)
verify   := (&ucpsig.VerifyMiddleware{Verifier: ucpsig.NewVerifier(resolver)}).Wrap
mux.Handle("/webhooks/checkout", verify(myWebhookHandler))
```

## Business side (verifying inbound, signing responses + outbound webhooks)

```go
priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
jwk, _  := ucpsig.JWKFromPublicKey("merchant-2026-01", &priv.PublicKey)
signer, _ := ucpsig.NewSigner(priv, jwk)

resolver := ucpsig.NewHTTPProfileResolver(nil, 5*time.Minute)
// Optional: only accept signatures from known platform profiles.
resolver.Trust = func(url string) bool {
    return url == "https://platform.example/.well-known/ucp"
}
verify  := (&ucpsig.VerifyMiddleware{Verifier: ucpsig.NewVerifier(resolver)}).Wrap
signOut := ucpsig.SignResponse(signer, nil)

// Order matters: outer verify, inner sign-response.
mux.Handle("/checkout-sessions", verify(signOut(http.HandlerFunc(handleCheckout))))

// Send a signed webhook back:
webhookClient := &http.Client{Transport: &ucpsig.SigningTransport{
    Signer:     signer,
    ProfileURL: "https://business.example/.well-known/ucp",
}}
```

## Running the demos

```bash
go build -o /tmp/biz  ./example/business
go build -o /tmp/plat ./example/platform

/tmp/biz  &       # business on :8081
sleep 1
/tmp/plat &       # platform on :8080
```

Expected output: business logs `verified checkout request from …`, platform logs `response signature verified ✓` and `inbound webhook verified: …`.

## Design notes

- **Idempotency-Key is enforced at the signer.** Per the spec, replay protection lives at the business layer through idempotency keys, and the key MUST be in the signed component set for state-changing methods. `Signer.SignRequest` refuses to sign POST/PUT/PATCH/DELETE without one.
- **Raw `r‖s` ECDSA, not DER.** `ecdsa.go` builds the signature as two big-endian fixed-width integers per the curve (32+32 for P-256, 48+48 for P-384). Many libraries (OpenSSL/Java/.NET) default to DER and break interop here.
- **Bodies are digested as raw bytes.** No JSON canonicalisation. Intermediaries (gateways, proxies) must not re-serialise — the spec is explicit and `VerifyMiddleware` will return `digest_mismatch` if they do.
- **Profile resolver does in-flight deduplication.** A burst of requests for the same unknown profile produces one outbound fetch, not N.
- **Trust allow-list is on the resolver, not the verifier.** This lets you keep verification logic clean while pinning the set of acceptable counterparties at the I/O boundary.
- **Response signing buffers the body.** The signature base needs the `Content-Digest`, which needs the full body, so the middleware buffers until handler return. Don't use it for streaming responses.

## Production checklist

- [ ] Replace `crypto/rand.GenerateKey` with Vault Transit-backed keys (or another KMS). The `Signer` interface accepts an `*ecdsa.PrivateKey`; swap in a `crypto.Signer` wrapper if you can't export the scalar.
- [ ] Set `AllowInsecureHTTP = false` (the default) and use real HTTPS profile URLs.
- [ ] Configure `HTTPProfileResolver.Trust` on the business side with your platform allow-list.
- [ ] Wire `Idempotency-Key` to your durable idempotency store (24–48h TTL per spec). The signer ensures it's signed; storing and replaying cached responses is your handler's job.
- [ ] Implement key rotation: publish the new key in `signing_keys[]` before swapping the `Signer`, keep the old key in the published list for ≥7 days, then drop it.
- [ ] Cap `Verifier.MaxBodyBytes` to your largest expected payload — the default is 5 MiB.
