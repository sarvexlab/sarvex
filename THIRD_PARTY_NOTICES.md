# Third-party notices

Sarvex is licensed under the Apache License 2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). It depends on the third-party software listed below, each under its own licence. This file is generated from the module graph and the console's production dependency tree; regenerate it with `scripts/check-licenses.py --tsv` when dependencies change.

All dependencies use permissive licences (Apache-2.0, MIT, BSD, ISC) except the HashiCorp libraries below, which are MPL-2.0. MPL-2.0 is file-level copyleft: Sarvex uses them unmodified as libraries, so the obligations are limited to keeping their licence notices and making their source available (it is public at the links shown).

## Go modules

| Module | Version | Licence |
|---|---|---|
| `github.com/MicahParks/jwkset` | v0.11.0 | Apache-2.0 |
| `github.com/MicahParks/keyfunc/v3` | v3.7.0 | Apache-2.0 |
| `github.com/ThreeDotsLabs/watermill` | v1.5.2 | MIT |
| `github.com/ThreeDotsLabs/watermill-nats/v2` | v2.2.0 | MIT |
| `github.com/cenkalti/backoff/v4` | v4.3.0 | MIT |
| `github.com/cenkalti/backoff/v5` | v5.0.3 | MIT |
| `github.com/cespare/xxhash/v2` | v2.3.0 | MIT |
| `github.com/fsnotify/fsnotify` | v1.6.0 | BSD-3-Clause |
| `github.com/ghodss/yaml` | v1.0.0 | MIT |
| `github.com/go-jose/go-jose/v4` | v4.1.4 | Apache-2.0 |
| `github.com/go-logr/logr` | v1.4.4 | Apache-2.0 |
| `github.com/go-logr/stdr` | v1.2.2 | Apache-2.0 |
| `github.com/golang-jwt/jwt/v5` | v5.3.0 | MIT |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause |
| `github.com/grpc-ecosystem/grpc-gateway/v2` | v2.30.0 | BSD-3-Clause |
| `github.com/hashicorp/errwrap` | v1.1.0 | MPL-2.0 |
| `github.com/hashicorp/go-cleanhttp` | v0.5.2 | MPL-2.0 |
| `github.com/hashicorp/go-multierror` | v1.1.1 | MPL-2.0 |
| `github.com/hashicorp/go-retryablehttp` | v0.7.8 | MPL-2.0 |
| `github.com/hashicorp/go-rootcerts` | v1.0.2 | MPL-2.0 |
| `github.com/hashicorp/go-secure-stdlib/parseutil` | v0.2.0 | MPL-2.0 |
| `github.com/hashicorp/go-secure-stdlib/strutil` | v0.1.2 | MPL-2.0 |
| `github.com/hashicorp/go-sockaddr` | v1.0.7 | MPL-2.0 |
| `github.com/hashicorp/hcl` | v1.0.1-vault-7 | MPL-2.0 |
| `github.com/hashicorp/vault/api` | v1.23.0 | MPL-2.0 |
| `github.com/hibiken/asynq` | v0.26.0 | MIT |
| `github.com/joho/godotenv` | v1.5.1 | MIT |
| `github.com/kinde-oss/kinde-go` | v0.2.0 | MIT |
| `github.com/klauspost/compress` | v1.18.6 | Apache-2.0 |
| `github.com/labstack/echo/v4` | v4.15.4 | MIT |
| `github.com/labstack/gommon` | v0.5.0 | MIT |
| `github.com/lithammer/shortuuid/v3` | v3.0.7 | MIT |
| `github.com/magiconair/properties` | v1.8.6 | BSD-2-Clause |
| `github.com/matoous/go-nanoid/v2` | v2.0.0 | MIT |
| `github.com/mattn/go-colorable` | v0.1.15 | MIT |
| `github.com/mattn/go-isatty` | v0.0.22 | MIT |
| `github.com/mitchellh/mapstructure` | v1.5.0 | MIT |
| `github.com/nats-io/nats.go` | v1.52.0 | Apache-2.0 |
| `github.com/nats-io/nkeys` | v0.4.15 | Apache-2.0 |
| `github.com/nats-io/nuid` | v1.0.1 | Apache-2.0 |
| `github.com/oklog/ulid` | v1.3.1 | Apache-2.0 |
| `github.com/pelletier/go-toml/v2` | v2.2.4 | MIT |
| `github.com/pkg/errors` | v0.9.1 | BSD-2-Clause |
| `github.com/redis/go-redis/v9` | v9.21.0 | BSD-2-Clause |
| `github.com/resend/resend-go/v2` | v2.28.0 | MIT |
| `github.com/robfig/cron/v3` | v3.0.1 | MIT |
| `github.com/ryanuber/go-glob` | v1.0.0 | MIT |
| `github.com/samber/lo` | v1.53.0 | MIT |
| `github.com/sony/gobreaker` | v1.0.0 | MIT |
| `github.com/spf13/afero` | v1.15.0 | Apache-2.0 |
| `github.com/spf13/cast` | v1.10.0 | MIT |
| `github.com/spf13/cobra` | v1.6.1 | Apache-2.0 |
| `github.com/spf13/jwalterweatherman` | v1.1.0 | MIT |
| `github.com/spf13/pflag` | v1.0.9 | BSD-3-Clause |
| `github.com/spf13/viper` | v1.13.0 | MIT |
| `github.com/stretchr/testify` | v1.12.1 | MIT |
| `github.com/stripe/stripe-go/v72` | v72.122.0 | MIT |
| `github.com/subosito/gotenv` | v1.4.1 | MIT |
| `github.com/tkanos/gonfig` | v0.0.0-20210106201359-53e13348de2f | MIT |
| `github.com/valyala/bytebufferpool` | v1.0.0 | MIT |
| `github.com/valyala/fasttemplate` | v1.2.2 | MIT |
| `github.com/xdg-go/scram` | v1.2.0 | Apache-2.0 |
| `github.com/xdg-go/stringprep` | v1.0.4 | Apache-2.0 |
| `github.com/youmark/pkcs8` | v0.0.0-20240726163527-a2c0da244d78 | MIT |
| `go.mongodb.org/mongo-driver/v2` | v2.6.0 | Apache-2.0 |
| `go.opencensus.io` | v0.24.0 | Apache-2.0 |
| `go.opentelemetry.io/auto/sdk` | v1.2.1 | Apache-2.0 |
| `go.opentelemetry.io/otel` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/metric` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/sdk` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/sdk/metric` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/otel/trace` | v1.46.0 | Apache-2.0 |
| `go.opentelemetry.io/proto/otlp` | v1.11.0 | Apache-2.0 |
| `go.uber.org/atomic` | v1.11.0 | MIT |
| `go.yaml.in/yaml/v3` | v3.0.5 | Apache-2.0 |
| `golang.org/x/crypto` | v0.57.0 | BSD-3-Clause |
| `golang.org/x/net` | v0.60.0 | BSD-3-Clause |
| `golang.org/x/oauth2` | v0.36.0 | BSD-3-Clause |
| `golang.org/x/sync` | v0.23.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.48.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.42.0 | BSD-3-Clause |
| `golang.org/x/time` | v0.15.0 | BSD-3-Clause |
| `google.golang.org/genproto/googleapis/api` | v0.0.0-20260819154853-08b0e4226688 | Apache-2.0 |
| `google.golang.org/genproto/googleapis/rpc` | v0.0.0-20260819154853-08b0e4226688 | Apache-2.0 |
| `google.golang.org/grpc` | v1.83.1 | Apache-2.0 |
| `google.golang.org/protobuf` | v1.36.12 | BSD-3-Clause |
| `gopkg.in/go-playground/assert.v1` | v1.2.1 | MIT |
| `gopkg.in/ini.v1` | v1.67.0 | Apache-2.0 |
| `gopkg.in/yaml.v2` | v2.4.0 | Apache-2.0 |
| `gopkg.in/yaml.v3` | v3.0.1 | Apache-2.0 |

## Console (npm, production dependencies)

| Package | Version | Licence |
|---|---|---|
| `@babel/helper-string-parser` | 7.29.7 | MIT |
| `@babel/helper-validator-identifier` | 7.29.7 | MIT |
| `@babel/parser` | 7.29.9 | MIT |
| `@babel/types` | 7.29.8 | MIT |
| `@intlify/core-base` | 11.4.8 | MIT |
| `@intlify/devtools-types` | 11.4.8 | MIT |
| `@intlify/message-compiler` | 11.4.8 | MIT |
| `@intlify/shared` | 11.4.8 | MIT |
| `@jridgewell/sourcemap-codec` | 1.6.0 | MIT |
| `@kinde-oss/kinde-auth-pkce-js` | 4.5.0 | MIT |
| `@kinde/js-utils` | 0.30.2 | MIT |
| `@kinde/jwt-decoder` | 0.2.3 | MIT* |
| `@kurkle/color` | 0.3.4 | MIT |
| `@vue/compiler-core` | 3.5.43 | MIT |
| `@vue/compiler-dom` | 3.5.43 | MIT |
| `@vue/compiler-sfc` | 3.5.43 | MIT |
| `@vue/compiler-ssr` | 3.5.43 | MIT |
| `@vue/devtools-api` | 6.6.4 | MIT |
| `@vue/reactivity` | 3.5.43 | MIT |
| `@vue/runtime-core` | 3.5.43 | MIT |
| `@vue/runtime-dom` | 3.5.43 | MIT |
| `@vue/server-renderer` | 3.5.43 | MIT |
| `@vue/shared` | 3.5.43 | MIT |
| `agent-base` | 6.0.2 | MIT |
| `asynckit` | 0.4.0 | MIT |
| `axios` | 1.20.0 | MIT |
| `call-bind-apply-helpers` | 1.0.2 | MIT |
| `chart.js` | 4.5.1 | MIT |
| `combined-stream` | 1.0.8 | MIT |
| `csstype` | 3.2.3 | MIT |
| `debug` | 4.4.3 | MIT |
| `delayed-stream` | 1.0.0 | MIT |
| `dunder-proto` | 1.0.1 | MIT |
| `entities` | 7.0.1 | BSD-2-Clause |
| `es-define-property` | 1.0.1 | MIT |
| `es-errors` | 1.3.0 | MIT |
| `es-object-atoms` | 1.1.2 | MIT |
| `es-set-tostringtag` | 2.1.0 | MIT |
| `estree-walker` | 2.0.2 | MIT |
| `follow-redirects` | 1.16.0 | MIT |
| `form-data` | 4.0.6 | MIT |
| `function-bind` | 1.1.2 | MIT |
| `get-intrinsic` | 1.3.0 | MIT |
| `get-proto` | 1.0.1 | MIT |
| `gopd` | 1.2.0 | MIT |
| `has-symbols` | 1.1.0 | MIT |
| `has-tostringtag` | 1.0.2 | MIT |
| `hasown` | 2.0.4 | MIT |
| `https-proxy-agent` | 5.0.1 | MIT |
| `jwt-decode` | 4.0.0 | MIT |
| `magic-string` | 0.30.21 | MIT |
| `math-intrinsics` | 1.1.0 | MIT |
| `mime-db` | 1.52.0 | MIT |
| `mime-types` | 2.1.35 | MIT |
| `ms` | 2.1.3 | MIT |
| `nanoid` | 3.3.19 | MIT |
| `picocolors` | 1.1.1 | ISC |
| `pinia` | 2.3.1 | MIT |
| `postcss` | 8.5.28 | MIT |
| `proxy-from-env` | 2.1.0 | MIT |
| `source-map-js` | 1.2.2 | BSD-3-Clause |
| `typescript` | 5.6.3 | Apache-2.0 |
| `vue-chartjs` | 5.3.4 | MIT |
| `vue-demi` | 0.14.10 | MIT |
| `vue-i18n` | 11.4.8 | MIT |
| `vue-router` | 4.6.4 | MIT |
| `vue` | 3.5.43 | MIT |

`@kinde/jwt-decoder` declares `MIT*` in its package metadata; its repository carries the MIT licence text.

Development-only tooling (test runner, type checker, bundler, deployment CLI) is not distributed with Sarvex and is not listed. Note that some optional image-processing binaries pulled in by the Cloudflare `wrangler` deployment CLI are LGPL-3.0-or-later; they are only used on a developer machine to deploy the console and are never shipped.

## Fonts

- **Geist** and **Geist Mono** — Copyright (c) 2023 Vercel, in collaboration with basement.studio. SIL Open Font License 1.1: [web/console/public/fonts/OFL.txt](web/console/public/fonts/OFL.txt).

## Specifications

- **Universal Commerce Protocol** schemas (https://github.com/Universal-Commerce-Protocol/ucp) — Apache-2.0. Go types in `internal/spec/` are derived from them.

## PHP plugins

The WooCommerce and Magento plugins have no runtime Composer dependencies beyond the host platform (WordPress/WooCommerce, Magento 2). PHPUnit (BSD-3-Clause) is used for development only.

