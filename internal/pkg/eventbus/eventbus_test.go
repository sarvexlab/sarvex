// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// ExampleNew_publisher demonstrates publishing events from a service.
func ExampleNew_publisher() {
	bus, err := New(Config{
		ConsumerGroup: "merchant-service",
		NATS:          NATSConfig{URL: "nats://localhost:4222"},
		Logger:        StdLogger(false),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	ctx := context.Background()

	evt := NewMerchantCreated("merchant-abc", MerchantCreatedPayload{
		Name:  "Acme Corp",
		Email: "hello@acme.com",
		Plan:  "pro",
	})

	if err := bus.Publish(ctx, evt); err != nil {
		log.Printf("publish failed: %v", err)
	}
}

// ExampleNew_subscriber demonstrates subscribing to events in a service.
func ExampleNew_subscriber() {
	bus, err := New(Config{
		ConsumerGroup: "notification-service",
		NATS:          NATSConfig{URL: "nats://localhost:4222"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	ctx := context.Background()

	sub, err := bus.Subscribe(ctx, "merchant.created", func(ctx context.Context, event DomainEvent) error {
		var payload MerchantCreatedPayload
		if err := json.Unmarshal(event.Payload(), &payload); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}

		fmt.Printf("new merchant: %s (%s)\n", payload.Name, payload.Email)
		return nil // ACK
	})
	if err != nil {
		log.Fatal(err)
	}

	// Run for a while, then unsubscribe.
	time.Sleep(5 * time.Second)

	if err := bus.Unsubscribe(ctx, sub); err != nil {
		log.Printf("unsubscribe: %v", err)
	}
}

// ExampleNew_pubAndSub shows a process acting as both publisher and subscriber,
// which is the typical pattern for any non-trivial service.
func ExampleNew_pubAndSub() {
	bus, err := New(Config{
		ConsumerGroup: "order-service",
		NATS:          NATSConfig{URL: "nats://localhost:4222"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer bus.Close()

	ctx := context.Background()

	// Subscribe to merchant events from another service.
	_, err = bus.Subscribe(ctx, "merchant.created", func(ctx context.Context, event DomainEvent) error {
		fmt.Printf("order service: merchant %s created, provisioning defaults\n", event.MerchantID())
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// Also publish payment events produced by this service.
	evt := NewPaymentProcessed("merchant-abc", PaymentProcessedPayload{
		OrderID:  "order-123",
		Amount:   99.99,
		Currency: "USD",
	})

	if err := bus.Publish(ctx, evt); err != nil {
		log.Printf("publish: %v", err)
	}

	// Block until terminated.
	select {}
}

// ExampleNew_multipleSubscribers demonstrates fan-out: three different consumer
// groups each receive their own copy of every merchant.created event.
func ExampleNew_multipleSubscribers() {
	newBus := func(group string) EventBus {
		b, err := New(Config{
			ConsumerGroup: group,
			NATS:          NATSConfig{URL: "nats://localhost:4222"},
		})
		if err != nil {
			log.Fatal(err)
		}
		return b
	}

	ctx := context.Background()

	notificationBus := newBus("notification-service")
	analyticsBus := newBus("analytics-service")
	auditBus := newBus("audit-service")

	defer notificationBus.Close()
	defer analyticsBus.Close()
	defer auditBus.Close()

	notificationBus.Subscribe(ctx, "merchant.created", func(_ context.Context, e DomainEvent) error { //nolint:errcheck
		fmt.Println("notifications: send welcome email to", e.MerchantID())
		return nil
	})

	analyticsBus.Subscribe(ctx, "merchant.created", func(_ context.Context, e DomainEvent) error { //nolint:errcheck
		fmt.Println("analytics: record merchant signup", e.MerchantID())
		return nil
	})

	auditBus.Subscribe(ctx, "merchant.created", func(_ context.Context, e DomainEvent) error { //nolint:errcheck
		fmt.Println("audit: log event", e.EventType(), e.OccurredAt())
		return nil
	})

	// Publish once — all three services receive it.
	publisherBus := newBus("merchant-service")
	defer publisherBus.Close()

	publisherBus.Publish(ctx, NewMerchantCreated("merchant-xyz", MerchantCreatedPayload{ //nolint:errcheck
		Name: "Beta Corp",
	}))

	time.Sleep(2 * time.Second)
}

// MerchantCreatedPayload is the structured body of a merchant.created event.
type MerchantCreatedPayload struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Plan  string `json:"plan"`
}

// MerchantCreated is a domain event emitted when a new merchant is registered.
type MerchantCreated struct {
	merchantID string
	occurredAt time.Time
	payload    MerchantCreatedPayload
}

func NewMerchantCreated(merchantID string, p MerchantCreatedPayload) *MerchantCreated {
	return &MerchantCreated{
		merchantID: merchantID,
		occurredAt: time.Now().UTC(),
		payload:    p,
	}
}

func (e *MerchantCreated) EventType() string     { return "merchant.created" }
func (e *MerchantCreated) MerchantID() string    { return e.merchantID }
func (e *MerchantCreated) StoreID() string       { return e.merchantID + "-default" }
func (e *MerchantCreated) OccurredAt() time.Time { return e.occurredAt }
func (e *MerchantCreated) Payload() []byte {
	b, _ := json.Marshal(e.payload)
	return b
}

// Compile-time check that MerchantCreated satisfies the Event interface.
var _ DomainEvent = (*MerchantCreated)(nil)

// PaymentProcessedPayload is the body of a payment.processed event.
type PaymentProcessedPayload struct {
	OrderID  string  `json:"order_id"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// PaymentProcessed is emitted by the payments service when a charge succeeds.
type PaymentProcessed struct {
	merchantID string
	occurredAt time.Time
	payload    PaymentProcessedPayload
}

func NewPaymentProcessed(merchantID string, p PaymentProcessedPayload) *PaymentProcessed {
	return &PaymentProcessed{
		merchantID: merchantID,
		occurredAt: time.Now().UTC(),
		payload:    p,
	}
}

func (e *PaymentProcessed) EventType() string     { return "payment.processed" }
func (e *PaymentProcessed) MerchantID() string    { return e.merchantID }
func (e *PaymentProcessed) StoreID() string       { return e.merchantID + "-default" }
func (e *PaymentProcessed) OccurredAt() time.Time { return e.occurredAt }
func (e *PaymentProcessed) Payload() []byte {
	b, _ := json.Marshal(e.payload)
	return b
}

var _ DomainEvent = (*PaymentProcessed)(nil)
