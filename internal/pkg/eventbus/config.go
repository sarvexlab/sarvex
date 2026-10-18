// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package eventbus

// Config holds all configuration needed to construct an EventBus. The
// transport is NATS JetStream everywhere — local development, CI and
// production run the same backend.
type Config struct {
	// ConsumerGroup identifies this logical service when competing for messages.
	// All instances of the same service should share the same ConsumerGroup so
	// that each event is handled by exactly one instance (competing consumers).
	// Different services should use different ConsumerGroups so each receives
	// a copy (fan-out).
	ConsumerGroup string

	// TopicPrefix is prepended to every topic name, e.g. "sarvex-" →
	// "sarvex-merchant.created". Useful for separating environments that
	// share one NATS deployment.
	TopicPrefix string

	// NATS holds JetStream connection settings.
	NATS NATSConfig

	// Logger is an optional structured logger. Defaults to a no-op logger.
	Logger Logger
}

// NATSConfig configures the NATS JetStream backend.
type NATSConfig struct {
	// URL is the NATS server (or supercluster leaf-node-local) address,
	// e.g. "nats://localhost:4222". A region's serving plane points this
	// at its own regional NATS leaf node, not directly at the core
	// supercluster — see deployments/selfhost/scripts/bootstrap-nats-leaf.sh.
	URL string
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.ConsumerGroup == "" {
		c.ConsumerGroup = "default"
	}
	if c.NATS.URL == "" {
		c.NATS.URL = "nats://localhost:4222"
	}
	if c.Logger == nil {
		c.Logger = &noopLogger{}
	}
}
