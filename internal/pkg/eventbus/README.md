# eventbus

A publisher/subscriber event bus for Go, built on [Watermill](https://watermill.io).

- **NATS JetStream** — the same backend in production, local development and CI
- **At-least-once delivery** — handlers return `error` to NACK and trigger redelivery
- **Fan-out** — different consumer groups each receive their own copy
- **Competing consumers** — same consumer group across instances shares load
- **Isolated processes** — publish and subscribe independently, mix in a single process

---

## Installation

```sh
go get github.com/yourorg/eventbus
```

---

## Implementing an Event

```go
type MerchantCreated struct {
    merchantID string
    occurredAt time.Time
    payload    []byte
}

func (e *MerchantCreated) EventType() string      { return "merchant.created" }
func (e *MerchantCreated) MerchantID() string     { return e.merchantID }
func (e *MerchantCreated) OccurredAt() time.Time  { return e.occurredAt }
func (e *MerchantCreated) Payload() []byte        { return e.payload }
```

---

## Constructing a bus

```go
import _ "github.com/sarvexlabs/sarvex.cp/internal/pkg/eventbus/natsjetstream" // registers the backend

bus, err := eventbus.New(eventbus.Config{
    ConsumerGroup: "my-service",
    NATS:          eventbus.NATSConfig{URL: "nats://localhost:4222"},
    // TopicPrefix: "staging." — separates environments sharing one NATS deployment
})
```

Streams and durable consumers are created automatically on first use.

For local development, `make infra-up` starts NATS (and the other backing
services) from `deployments/docker-compose.yaml`.

---

## Publishing

```go
ctx := context.Background()

if err := bus.Publish(ctx, &MerchantCreated{...}); err != nil {
    log.Printf("publish failed: %v", err)
}
```

---

## Subscribing

```go
sub, err := bus.Subscribe(ctx, "merchant.created", func(ctx context.Context, evt eventbus.Event) error {
    var p MyPayload
    if err := json.Unmarshal(evt.Payload(), &p); err != nil {
        return err // NACK — will be redelivered
    }
    // process...
    return nil // ACK
})
```

### Unsubscribing

```go
bus.Unsubscribe(ctx, sub)
```

---

## Fan-out vs competing consumers

| Scenario | Configuration |
|---|---|
| Every service gets a copy | Use a **different** `ConsumerGroup` per service |
| Load-balance across instances | Use the **same** `ConsumerGroup` across all instances |

---

## At-least-once delivery

Handlers that return a non-nil error cause the message to be NACKed. The backend redelivers it automatically. The Watermill router is configured with:

- **5 retries** with immediate redeliver on transient errors
- **RecoverMiddleware** — panics in handlers are caught and converted to NACKs
- **CorrelationID propagation** — message UUID is available via `watermill.MessageUUIDKey` in the handler context

Design your handlers to be **idempotent** — duplicate delivery is possible in all at-least-once systems.

---

## Running tests

Unit tests use an in-process Go channel backend (no external services needed):

```sh
go test ./...
```

Integration tests run against the NATS JetStream container from
`deployments/docker-compose.yaml`:

```sh
make infra-up
NATS_URL=nats://localhost:4222 go test -tags integration ./...
```

---

## Package layout

```
eventbus/
├── eventbus.go           — EventBus, Event, EventHandler, Subscription interfaces
├── config.go             — Config, NATSConfig
├── bus.go                — concrete bus implementation
├── backend.go            — backend wiring, RegisterNATSBackend
├── envelope.go           — wire format serialisation/deserialisation
├── subscription.go       — Subscription implementation
├── logger.go             — Logger interface and helpers
├── bus_test.go           — unit tests (in-process backend)
├── eventbus_test.go      — usage examples
└── natsjetstream/        — NATS JetStream backend, in its own package so importing
                            eventbus alone (e.g. for the EventBus interface) never
                            pulls in the NATS client; a caller that needs the bus to
                            work at runtime must blank-import this subpackage
```
