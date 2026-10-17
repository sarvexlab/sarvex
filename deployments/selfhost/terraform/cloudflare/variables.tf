variable "zone_id" {
  description = "Cloudflare zone ID for the domain Sarvex is served under."
  type        = string
}

variable "root_domain" {
  description = "e.g. sarvex.example — hostnames below are built as <sub>.<root_domain>."
  type        = string
}

# One entry per region — add-region.sh appends here (via regions.auto.tfvars.json)
# rather than anyone hand-editing this file or clicking through the
# Cloudflare dashboard's Load Balancing UI. "core" must always be present;
# every other key is an edge region.
variable "regions" {
  description = "Map of region name -> its serving-plane origin (Gateway) address."
  type = map(object({
    origin_address = string # public IP or hostname of the region's Gateway/LB
    is_primary     = optional(bool, false)
  }))
}
