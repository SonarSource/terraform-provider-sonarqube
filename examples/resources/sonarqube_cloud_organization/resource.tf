resource "sonarqube_cloud_organization" "example" {
  key         = "my-organization"
  name        = "My organization"
  description = "Projects of the platform team"
  url         = "https://example.com/platform"
}
