# Governance

Sarvex is an open-source project started and stewarded by Sarvex Labs. This document describes how decisions are made today; it will evolve as the contributor community grows.

## Roles

- **Users and contributors** — anyone who uses the project, files issues, sends pull requests or helps others. No formal status is required.
- **Maintainers** — contributors with merge rights and responsibility for a part of the codebase or the whole project (see [MAINTAINERS.md](MAINTAINERS.md)).

## Becoming a maintainer

A contributor who has sustained, high-quality involvement (reviews, fixes, features, triage) over several months can be nominated by any maintainer. A nomination is accepted when a majority of current maintainers agree and no maintainer objects on technical or conduct grounds. Maintainers who are inactive for 12 months may be moved to emeritus status.

## Decision making

- Day-to-day changes are decided in pull requests by lazy consensus: a change merges once a maintainer has approved it, CI is green, and no maintainer has objected.
- Changes to the architectural invariants in [A10 of the architecture](sarvex-v1-architecture.md#a10-backend-invariants), to the public contracts (`sarvex-v1-*.yaml`), or to licensing require an Architecture Decision Record in [`docs/adr`](docs/adr) and approval from a majority of maintainers.
- If consensus cannot be reached, the maintainers vote; Sarvex Labs' maintainers break ties.

## Releases

Releases are cut from `main` by a maintainer and recorded in [CHANGELOG.md](CHANGELOG.md). Breaking changes to wire formats or contracts are called out explicitly there.

## Conduct and security

Conduct reports follow the [Code of Conduct](CODE_OF_CONDUCT.md); vulnerability reports follow [SECURITY.md](SECURITY.md).

## Licence and trademarks

The code is licensed under Apache-2.0 and contributions are accepted under the same licence. The Sarvex name and logo are marks of Sarvex Labs; use of the marks to identify unmodified software or to describe compatibility is fine, other uses need permission.
