resource "sonarqube_cloud_organization_permissions" "developers" {
  organization = "my-org"
  group_name   = "Developers"
  permissions  = ["scan", "gateadmin"]
}
