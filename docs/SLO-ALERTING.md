# SLO-Based Alerting Rules (F-046)

Service Level Objectives (SLOs) and alerting thresholds for Sarvex V1 production monitoring.
These rules are based on metrics emitted by the OTEL instrumentation (F-043) wired into all three planes.

## Overview

Each plane exports metrics via OpenTelemetry to a metrics backend (configured via OTEL_* environment variables).
SLOs define acceptable levels of service availability, latency, and error rates. Alerts fire when SLOs are at risk.

---

## Serving Plane SLOs

**Responsibilities:** UCP profile serving, discovery, pricing, catalog reads, checkout sessions, negotiation.

### Availability SLO: 99.9%

**Definition:** Request success rate (2xx/3xx/4xx) ≥ 99.9% over 30-day rolling window.  
**Alert condition:** Error rate (5xx) > 0.1% over 5-minute window.  
**Metric:** `http.server.request.duration` (status=5xx)  
**Action:** Page on-call. Check serving-plane logs and Redis serving-cache health.

### Latency SLO: p99 < 200ms

**Definition:** 99th percentile request latency < 200ms for non-checkout requests (checkout sessions are latency-sensitive but exempt from this SLO per architecture).  
**Alert condition:** p99 latency > 400ms sustained for 5 minutes.  
**Metric:** `http.server.request.duration` (quantile=0.99)  
**Action:** Page on-call. Check database query latency, Redis cache hit rate, downstream connector latency.

---

## Sync Plane SLOs

**Responsibilities:** Signal ingestion, catalog import, mapping/scoring, feed generation, sweep reconciliation.

### Signal Processing SLO: 99% ingestion success

**Definition:** Successfully ingested signals ≥ 99% of received signals over 1-hour rolling window.  
**Alert condition:** Signal rejection/failure rate > 1% sustained for 10 minutes.  
**Metric:** `sync.signal.ingested` vs. `sync.signal.rejected` (by store_id)  
**Action:** Page on-call. Check connector health, catalog-store write availability, event-bus capacity.

### Catalog Import SLO: 95% completion rate

**Definition:** Initiated catalog imports complete successfully ≥ 95% over 24-hour window.  
**Alert condition:** Import failure rate > 5% sustained for 30 minutes.  
**Metric:** `sync.import.completed` vs. `sync.import.failed` (by store_id)  
**Action:** Page on-call. Check connector store-API availability, credential rotation, network paths.

---

## Control Plane SLOs

**Responsibilities:** Console API, operator tooling, task emission, validation.

### API Availability SLO: 99.5%

**Definition:** Console API success rate (2xx/3xx/4xx) ≥ 99.5% over 30-day window.  
**Alert condition:** Error rate (5xx) > 0.5% over 5-minute window.  
**Metric:** `http.server.request.duration` (path=/v1/*, status=5xx)  
**Action:** Page on-call. Check control-plane service health, database connectivity, event-bus delivery.

### Task Emission SLO: 99% delivery

**Definition:** Tasks raised by domains publish to the event bus successfully ≥ 99% over 1-hour window.  
**Alert condition:** Task emission failure rate > 1% sustained for 5 minutes.  
**Metric:** `control.task.emitted` vs. `control.task.emit_failed` (by domain)  
**Action:** Page on-call. Check event-bus capacity (NATS JetStream stream limits), authentication tokens.

### Console Responsiveness SLO: p95 < 500ms

**Definition:** 95th percentile console API latency < 500ms over 1-hour window.  
**Alert condition:** p95 latency > 1000ms sustained for 10 minutes.  
**Metric:** `http.server.request.duration` (path=/v1/*, quantile=0.95)  
**Action:** Page on-call. Check MongoDB query performance, network paths to merchants' stores, downstream connector latency.

---

## Cross-Plane SLOs

### Event Bus Delivery SLO: 99.99%

**Definition:** Events published to the bus complete successfully ≥ 99.99% (covers catalog changes, task emission, monitoring signals).  
**Alert condition:** Event publish failure rate > 0.01% sustained for 5 minutes.  
**Metric:** Watermill event-bus publish success/failure counters  
**Action:** Page on-call. Check NATS JetStream health, authentication, consumer backlogs.

### Database Availability SLO: 99.9%

**Definition:** MongoDB writes from all three planes complete successfully ≥ 99.9% over 1-hour window.  
**Alert condition:** Write failure rate > 0.1% sustained for 5 minutes.  
**Metric:** MongoDB driver error counters (by operation type)  
**Action:** Page on-call. Check MongoDB cluster health, network connectivity, replica set status.

---

## Alerting Thresholds (Quick Reference)

| Plane | Metric | Threshold | Duration | Severity |
|-------|--------|-----------|----------|----------|
| Serving | Error rate (5xx) | > 0.1% | 5m | P1 |
| Serving | Latency p99 | > 400ms | 5m | P2 |
| Sync | Signal rejection | > 1% | 10m | P1 |
| Sync | Import failure | > 5% | 30m | P2 |
| Sync | Feed age | > 4h | 4h | P3 |
| Control | Error rate (5xx) | > 0.5% | 5m | P1 |
| Control | Task emission failure | > 1% | 5m | P1 |
| Control | Latency p95 | > 1000ms | 10m | P2 |
| Cross-plane | Event bus failure | > 0.01% | 5m | P1 |
| Cross-plane | Database write failure | > 0.1% | 5m | P1 |

---

## Implementation Guide

### Prometheus (recommended for Kubernetes)

Export OTEL metrics to Prometheus via the Prometheus exporter. Define alert rules in `prometheus-rules.yml`:

```yaml
groups:
  - name: sarvex-serving
    rules:
      - alert: ServingPlaneErrorRateHigh
        expr: 'rate(http_server_request_duration_seconds_bucket{service="serving", status_code=~"5.."}[5m]) > 0.001'
        for: 5m
        annotations:
          summary: "Serving plane error rate > 0.1%"
```

### Custom Integration

If using a different monitoring platform (Datadog, New Relic, etc.):

1. Configure OTEL exporters to send to your platform via `OTEL_EXPORTER_OTLP_*` env vars
2. Map metric names and thresholds (see table above) to your platform's alert syntax
3. Ensure on-call routing includes all SLO violations

---

## Monitoring Checklist

- [ ] OTEL metrics flowing to monitoring backend (verify no OTEL exporter errors in logs)
- [ ] Alert policies created and tested in dry-run mode
- [ ] On-call team notified of new alert channels and thresholds
- [ ] Dashboard created for each plane showing current SLO compliance
- [ ] SLO tracking report scheduled (weekly/monthly) to identify trends
- [ ] Runbooks created for top 5 alert types (error-rate, latency, event-bus, database, feed-stale)

---

## SLO Review Cadence

- **Weekly:** Triage new alerts, identify patterns
- **Monthly:** Review SLO compliance vs. targets, adjust thresholds based on observed baselines
- **Quarterly:** Audit SLO definitions against user expectations and business impact
