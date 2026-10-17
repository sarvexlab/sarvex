# Local Vault server: persistent file storage, no TLS. Development only —
# production runs the Helm chart configured by deployments/selfhost/manifests.
storage "file" {
  path = "/vault/file"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true
}

api_addr      = "http://127.0.0.1:8200"
disable_mlock = true
ui            = true
