variable "enterprise_license" {
  type      = string
  sensitive = true
}

resource "sonarqube_cloud_enterprise" "main" {
  key          = "my-enterprise"
  name         = "My enterprise"
  license      = var.enterprise_license
  organization = "my-organization"

  lifecycle {
    prevent_destroy = true
  }
}
