resource "sonarqube_project" "example" {
  organization = "my-organization"
  key          = "my-organization_my-repo"
  name         = "My repository"
}

# The organization must be bound to GitHub, and the GitHub application
# installation of the organization must be able to see the repository.
resource "sonarqube_cloud_project_binding" "example" {
  organization = sonarqube_project.example.organization
  project_key  = sonarqube_project.example.key
  repository   = "my-github-org/my-repo"

  # A replacement of the project, such as for a new name, deletes the project
  # and its binding. This binds the new project in the same apply.
  lifecycle {
    replace_triggered_by = [sonarqube_project.example]
  }
}

# A destroy does not remove the binding. The API cannot do that: the binding
# goes away when the project is deleted.
