# Merchant-controlled platform trust allowlist

Each merchant controls which AI platforms may transact through their gateway instance. The allowlist is stored on `UCPInstance` and enforced by the gateway's `ucpsig.Verifier` before any backend call is made.

The `ucpsig.Verifier` uses a `Trust` function to decide which platform profile URLs it will fetch and verify against. In production this must be a real policy — the current code has a placeholder (`https://platform.example/.well-known/ucp`) that must be replaced before any merchant goes live.

We considered three alternatives: open trust (accept any valid UCP profile), a centrally-managed Sarvex allowlist, and merchant-controlled per-instance. Open trust is a security hole — any actor who can publish a UCP profile can transact against any merchant. A central Sarvex allowlist makes Sarvex a gatekeeper that bottlenecks platform onboarding. Merchant-controlled is the right threat model: a luxury goods merchant may want only verified platforms; a commodity merchant may want open access. It also aligns with how the rest of `UCPInstance` config works — merchants own their configuration.

## Consequences

- `UCPInstance` must carry a `TrustedPlatforms []string` field (profile URLs).
- The `resolver.Trust` function in `entry.go` must be wired to check the requesting instance's allowlist rather than a static value.
- An empty allowlist should be treated as deny-all, not allow-all. Merchants must explicitly add platforms before their instance is callable.
