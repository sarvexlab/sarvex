terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.0"
    }
  }
}

provider "cloudflare" {
  # CLOUDFLARE_API_TOKEN env var, per the provider's standard convention —
  # never hardcoded here or in a .tfvars file that could land in git.
}
