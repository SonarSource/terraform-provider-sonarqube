# The destroy of this resource does not change the default. Before you destroy
# a gate that is the default, set a different gate as the default.
resource "sonarqube_cloud_organization_default_quality_gate" "example" {
  organization    = "my-organization"
  quality_gate_id = sonarqube_cloud_quality_gate.example.id
}
