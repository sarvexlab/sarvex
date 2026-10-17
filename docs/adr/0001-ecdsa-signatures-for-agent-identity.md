# ECDSA signatures for agent identity, not API keys

AI platforms calling the gateway must sign requests using ECDSA keypairs per the UCP specification. We do not issue API keys to agents.

The UCP spec mandates cryptographic request signing as the identity primitive. API keys establish machine identity but provide no non-repudiation — a key leak silently grants full access with no forensic trail. ECDSA signatures are non-repudiable, verifiable without a database round-trip, and already implemented in the `ucpsig` package. The `ucpsig.Verifier` fetches the platform's public keys from their UCP profile URL and verifies each request signature against them.

## Considered options

**API keys**: Simpler to issue and rotate; familiar to developers. Rejected because they don't satisfy the UCP spec, provide no non-repudiation, and would require a database lookup per request to validate.

**API keys now, ECDSA later**: Considered as a Phase 1 shortcut. Rejected — the `ucpsig` package is already built, and migrating agents from API keys to ECDSA later would require breaking changes for all platform integrations.

## Consequences

- Platforms must implement UCP request signing to transact through the gateway.
- The `ucpsig.Verifier` fetches platform profiles from their `wellKnownURL`; the `resolver.Trust` function must be populated from the merchant's trusted platform allowlist on `UCPInstance` — see ADR 0003.
- Signature verification is disabled in non-production environments (`SignatureConfig.Disabled = !isProd`).
