resource "sonarqube_cloud_project_permissions" "developers" {
  organization = "my-org"
  project_key  = "my-project"
  group_name   = "Developers"
  permissions  = ["user", "codeviewer", "issueadmin"]
}
