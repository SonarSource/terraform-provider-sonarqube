data "sonarqube_project_binding" "example" {
  organization = "my-organization"
  project_key  = "my-organization_my-repo"
}

output "repository" {
  value = data.sonarqube_project_binding.example.repository
}
