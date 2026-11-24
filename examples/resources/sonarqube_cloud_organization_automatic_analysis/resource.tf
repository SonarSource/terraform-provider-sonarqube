# New projects of the organization get their analyses from a CI pipeline.
# The organization must be on the Enterprise plan. The destroy of this
# resource gives new projects the default of SonarQube Cloud again.
resource "sonarqube_cloud_organization_automatic_analysis" "example" {
  organization             = "my-organization"
  enabled_for_new_projects = false
}
