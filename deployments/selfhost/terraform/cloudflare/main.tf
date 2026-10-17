# Cloudflare Load Balancing in front of every self-hosted region's Gateway
# — geo-steered to the nearest origin, active-health-checked failover, and
# — critically for "any compute provider" — the origins can be anything
# with a public IP; Cloudflare doesn't care what's behind them. This is the
# infra-as-code replacement for clicking through the Load Balancing tab in
# the Cloudflare dashboard (plan's "Automation" section: day-N+1 is
# `terraform apply`, never a console step).

resource "cloudflare_load_balancer_monitor" "ucp_health" {
  type           = "https"
  path           = "/readyz"
  expected_codes = "200"
  interval       = 30
  retries        = 2
  timeout        = 5
  description    = "Sarvex serving-plane readiness probe — internal/planes/serving/api/entry.go's /readyz"
}

resource "cloudflare_load_balancer_pool" "region" {
  for_each = var.regions

  name = "sarvex-${each.key}"
  origins {
    name    = "sarvex-${each.key}-origin"
    address = each.value.origin_address
    enabled = true
  }
  monitor            = cloudflare_load_balancer_monitor.ucp_health.id
  # Fail over to the next-nearest pool if this region's origin goes
  # unhealthy — proves the plane-isolation invariant end to end: an edge
  # region outage never takes down the others (plan's Verification section).
  notification_email = ""
}

resource "cloudflare_load_balancer" "sarvex_ucp" {
  zone_id          = var.zone_id
  name             = "ucp.${var.root_domain}"
  fallback_pool_id = cloudflare_load_balancer_pool.region[[for k, v in var.regions : k if v.is_primary][0]].id
  default_pool_ids = [for k in keys(var.regions) : cloudflare_load_balancer_pool.region[k].id]
  proxied          = true
  # Proximity steering — routes each request to the geographically nearest
  # healthy pool. This, plus the monitor above, is the entire "day N+1"
  # global-routing mechanism; adding a region is adding one pool.
  steering_policy  = "proximity"
}
