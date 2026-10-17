# Sarvex UCP Magento Module

The Sarvex UCP Magento module (`Sarvex_Ucp`) integrates your Magento 2 store with Sarvex's Google UCP checkout platform. It mirrors `plugin/woocommerce`'s scope exactly (see CLAUDE.md §4 — "the plugin has two roles, keep it thin"):

1. **Signal Sending** — Notifies Sarvex when products or inventory change
2. **Profile Proxy** — Serves the UCP profile at `/.well-known/ucp`
3. **Pairing** — Securely pairs your store with Sarvex

No business logic lives here — no scoring, mapping, or UCP interpretation. Everything that smells like logic belongs server-side, in Sarvex's sync plane.

## Installation

### Option A: Composer (recommended)

```bash
composer require sarvex/module-ucp-gateway
bin/magento module:enable Sarvex_Ucp
bin/magento setup:upgrade
bin/magento cache:flush
```

### Option B: Manual (`app/code`)

1. Copy this directory to `app/code/Sarvex/Ucp/` in your Magento installation
2. Run:
   ```bash
   bin/magento module:enable Sarvex_Ucp
   bin/magento setup:upgrade
   bin/magento setup:di:compile
   bin/magento cache:flush
   ```

## Configuration

After installation, go to **Stores → Configuration → Services → Sarvex UCP Gateway**.

### Required Settings

#### Sarvex Endpoint URL
- **What it is:** The URL of your Sarvex backend server
- **Format:** `https://sarvex.example.com` (no trailing slash)
- **Required:** Yes, before pairing

#### Store ID / Connector ID / HMAC Secret
- Populated automatically by the pairing flow (see below)
- Read-only; the HMAC secret is stored encrypted via Magento's own config encryption (`Magento\Config\Model\Config\Backend\Encrypted`)

### How Pairing Works

Pairing is a two-step handshake, identical to the WooCommerce plugin's:

1. **Begin pairing** — `Sarvex\Ucp\Model\PairingManager::beginPairing()` calls Sarvex with a bearer token (from the Sarvex console/operator flow) and receives a one-time pairing code + connector ID.
2. **Complete pairing** — `PairingManager::completePairing()` exchanges that pairing code for durable connector credentials, HMAC-signing the request with the pairing code itself as the one-time secret. The pairing code then becomes the permanent HMAC secret for all future signed requests.

This module exposes the `PairingManager` service; wiring a specific admin action/button to drive it end-to-end (equivalent to the console-triggered flow the operator UI uses) is left to the Sarvex console integration, matching how the WooCommerce plugin also only exposes the read-only result of an externally-driven pairing.

## How It Works

### Product and Inventory Signals

Whenever a product save or a stock item save **commits** (bound to `catalog_product_save_commit_after` / `cataloginventory_stock_item_save_commit_after`, not the `_after` variants, so the signal never races Sarvex's pull against an uncommitted transaction), the module sends a lean signal to Sarvex:

```json
{"sku": "...", "entity_kind": "product"}
```

Sarvex pulls the full product/inventory details from your store in response — the signal contains only the SKU and entity kind, never product data, PII, or payment credentials (*thin events, fat pull*, per CLAUDE.md §4).

### Profile Serving

`/.well-known/ucp` is served by a dedicated router (`Sarvex\Ucp\Routing\WellKnownRouter`, registered ahead of Magento's standard router — see `etc/frontend/di.xml`) and controller (`Sarvex\Ucp\Controller\Wellknown\Ucp`):

- Fetches the profile live from Sarvex (`GET /connector/profile`, HMAC-signed) on every request
- Falls back to the last-known-good cached copy if Sarvex is unreachable
- Always returns `200` with Sarvex's bytes as-is — **never a redirect**
- Holds no private keys and authors nothing

#### Webserver-level fallback (optional hardening)

The custom router is self-contained and needs no webserver configuration. If your deployment's edge proxy or CDN intercepts `.well-known/*` paths before they reach Magento's `index.php` (common with some ACME/Let's Encrypt setups), add an explicit passthrough rewrite as a fallback, e.g. for nginx:

```nginx
location = /.well-known/ucp {
    rewrite ^ /index.php$request_uri last;
}
```

## Security Considerations

- **HMAC Signing** — All requests to Sarvex are signed with HMAC-SHA256 (`Sarvex\Ucp\Model\Hmac`), byte-identical to the WooCommerce plugin's scheme, so Sarvex's signature verification is platform-agnostic.
- **Credential Storage** — Store ID, Connector ID, and the endpoint URL are stored in `core_config_data`; the HMAC secret is stored **encrypted** via Magento's config encryption.
- **Sequence counters** — Persisted in `core_config_data` (not the object cache), so an admin `cache:flush` cannot reset them and cause Sarvex's inbound sequence-based de-dup (CLAUDE.md invariant 6) to treat a genuinely new signal as an already-seen retry.
- **Data in Signals** — Product/inventory signals contain only the SKU and entity type. No payment credentials or PII.

## Module Structure

```
plugin/magento/
├── registration.php              # Module registration
├── composer.json
├── etc/
│   ├── module.xml
│   ├── config.xml                # Default config values
│   ├── acl.xml                   # Admin ACL resource
│   ├── events.xml                # Observer bindings
│   ├── adminhtml/system.xml      # Stores > Configuration UI
│   └── frontend/
│       ├── routes.xml            # frontName for the Wellknown controller's resolution
│       └── di.xml                # Registers WellKnownRouter
├── Model/
│   ├── Config.php                # Store-scoped config read/write (get_option/update_option equivalent)
│   ├── Hmac.php                  # HMAC-SHA256 signing (matches WooCommerce's HMAC.php)
│   ├── SignalSender.php          # Product/inventory change signals
│   ├── PairingManager.php        # Pairing handshake
│   ├── ProfileClient.php         # Fetches the live profile from Sarvex
│   └── ProfileCache.php          # Last-known-good profile fallback cache
├── Observer/
│   ├── ProductSaveObserver.php   # catalog_product_save_commit_after
│   └── StockItemSaveObserver.php # cataloginventory_stock_item_save_commit_after
├── Routing/
│   └── WellKnownRouter.php       # Matches literal /.well-known/ucp
├── Controller/Wellknown/
│   └── Ucp.php                   # Serves the profile
├── Test/Unit/Model/
│   ├── HmacTest.php
│   └── PairingManagerTest.php
└── README.md                     # This file
```

## Development

### Running Tests

`Test/Unit/Model/HmacTest.php` has no Magento dependency and runs with a bare PHPUnit install:

```bash
cd plugin/magento
composer require --dev phpunit/phpunit
vendor/bin/phpunit Test/Unit/Model/HmacTest.php
```

The rest of the suite (`PairingManagerTest.php`, and the observer/controller tests) mocks `Magento\Framework\HTTP\Client\Curl` and other framework classes, so it requires a Magento development environment (`magento/framework` installed via `composer require sarvex/module-ucp-gateway` inside a real Magento project, or Magento's own component test harness) to execute — the same way the WooCommerce plugin's test suite requires a WordPress test harness (WP_Mock/Brain Monkey) not bundled in this repo.

> **Note on verification:** this module is written to Magento 2's documented module/router/observer/config conventions, but — unlike the WooCommerce plugin, which is exercised end-to-end by `internal/planes/sync/sim` and `test/e2e/journey` — this repository exercises the connector against the simulated Magento store surface (`internal/platform/magento/sim`), not a real Magento instance. Smoke-test the `/.well-known/ucp` routing and the pairing/signal flow against a real Magento instance before relying on it in production.

## Support

For issues or questions, check the module's logs via Magento's standard `var/log/system.log` / `var/log/exception.log` (all log lines are tagged `sarvex_ucp:`), then contact Sarvex support with the relevant excerpt (sanitize the HMAC secret before sharing).

## License

This module is part of Sarvex and is licensed under the [Apache License 2.0](../../LICENSE).
