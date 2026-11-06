resource "sonarqube_cloud_scoped_organization_token" "analysis" {
  organization             = "my-org"
  name                     = "ci-analysis"
  description              = "Analysis token for CI"
  organization_permissions = ["scan"]
}

resource "github_actions_organization_secret" "sonar_token" {
  visibility      = "private"
  secret_name     = "SONAR_TOKEN"
  plaintext_value = sonarqube_cloud_scoped_organization_token.analysis.token
}

# A token that can analyze two projects only.
resource "sonarqube_cloud_scoped_organization_token" "backend" {
  organization        = "my-org"
  name                = "backend-analysis"
  project_permissions = ["scan"]
  project_keys        = ["my-org_api", "my-org_worker"]
}
