# The project gets its analyses from a CI pipeline. The destroy of this
# resource does not change the setting of the project.
resource "sonarqube_cloud_project_automatic_analysis" "example" {
  organization = "my-organization"
  project_key  = "my-project"
  enabled      = false
}
