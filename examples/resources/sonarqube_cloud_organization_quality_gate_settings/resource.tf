resource "sonarqube_cloud_organization_quality_gate_settings" "example" {
  organization         = "my-org"
  ignore_small_changes = true
}
