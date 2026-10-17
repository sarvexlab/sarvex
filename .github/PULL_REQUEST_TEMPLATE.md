## Summary

<!-- What does this change and why? Link the issue it closes. -->

## Checklist

- [ ] Tests added or updated; `make test-unit` passes (and the console suite if `web/console` changed)
- [ ] Change holds on every spec generation and both platforms (WooCommerce and Magento), or the PR explains why not
- [ ] No plane imports another plane; every new query is tenant-fenced (`docs/TENANCY-PATTERNS.md`)
- [ ] Contracts (`sarvex-v1-*.yaml`) and generated console types are updated together if the API changed
- [ ] Docs updated; new source files carry the SPDX header (`python3 scripts/license-headers.py`)
- [ ] No secrets, credentials or real customer data in the diff
