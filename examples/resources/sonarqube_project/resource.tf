resource "sonarqube_project" "example" {
  organization = "my-organization"
  key          = "my-project"
  name         = "My Project"
  main_branch  = "main"
}
