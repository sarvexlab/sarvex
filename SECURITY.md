# Security policy

Sarvex sits between AI shopping agents and merchants' commerce backends, so we take vulnerabilities seriously — especially anything that could cross a tenant boundary, leak or forge a payment reference, bypass signature or HMAC verification, or let a caller act as another merchant.

## Reporting a vulnerability

**Do not open a public issue, pull request or discussion.**

Use GitHub's private vulnerability reporting: on this repository, open the **Security** tab and choose **Report a vulnerability**. If that is unavailable, contact a maintainer listed in [MAINTAINERS.md](MAINTAINERS.md) and ask for a private channel before sharing details.

Please include: the affected component and version/commit, a description of the impact, reproduction steps or a proof of concept, and any suggested fix. Never include real customer data or credentials.

## What to expect

- Acknowledgement within 3 business days.
- An initial assessment (accepted / need more information / declined, with reasons) within 10 business days.
- Fixes for confirmed issues are prepared privately, released together with an advisory, and credited to you if you wish.
- We ask for a reasonable embargo (typically up to 90 days) so a fix can ship before details are public.

## Supported versions

Sarvex is pre-1.0. Only the latest commit on `main` and the most recent tagged release receive security fixes.

## Scope notes

In scope: the three planes, the connector and plugin protocols (HMAC, pairing), the console, the WooCommerce and Magento plugins, and the deployment manifests in this repository.

Out of scope: vulnerabilities in third-party dependencies that are not reachable from Sarvex (report those upstream), issues requiring a compromised operator account or host, denial of service by volumetric traffic, and the archived code under `internal/archive`, `docs/archive` and `web/console/archive`.

## Hardening references

- [`docs/SECURITY-TEST-PLAN.md`](docs/SECURITY-TEST-PLAN.md) — threat areas and test cases.
- [`docs/TENANCY-PATTERNS.md`](docs/TENANCY-PATTERNS.md) — tenant isolation rules.
- The repository runs secret scanning (gitleaks), CodeQL and dependency updates in CI (`.github/workflows/security.yml`, `.github/dependabot.yml`).
