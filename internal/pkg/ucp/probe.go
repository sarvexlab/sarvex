// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package ucp

// ProbeHeader identifies a request as originating from the endpoint-health
// synthetic probe worker (internal/planes/control/endpointhealth) rather
// than a real agent. Set on every probe request; read by the serving
// plane's telemetry middleware to mark the resulting event synthetic, and
// by any analytics/ledger consumer that must exclude synthetic traffic.
// Lives in internal/pkg/ucp (not internal/planes/serving or
// internal/planes/control/endpointhealth) because both the serving plane
// (producer of telemetry) and the control plane (producer of probe
// traffic) need it, and a plane must never import another
// (internal/planes/doc.go).
const ProbeHeader = "X-Sarvex-Probe"
