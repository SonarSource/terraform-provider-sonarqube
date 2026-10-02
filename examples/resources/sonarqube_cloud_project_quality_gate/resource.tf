# The destroy of this resource removes the assignment. The project then uses
# the default quality gate of the organization.
resource "sonarqube_cloud_project_quality_gate" "example" {
  organization    = "my-organization"
  project_key     = "my-project"
  quality_gate_id = sonarqube_cloud_quality_gate.example.id
}
