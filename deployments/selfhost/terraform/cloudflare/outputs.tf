output "load_balancer_hostname" {
  value       = cloudflare_load_balancer.sarvex_ucp.name
  description = "Point this at your merchants/agents — Cloudflare steers each request to the nearest healthy region."
}

output "pool_ids" {
  value = { for k, v in cloudflare_load_balancer_pool.region : k => v.id }
}
