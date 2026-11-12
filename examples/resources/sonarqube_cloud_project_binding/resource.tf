resource "sonarqube_project" "example" {
  organization = "my-organization"
  key          = "my-organization_my-repo"
  name         = "My repository"
}

# The organization of this project is bound to GitHub, and the GitHub
# application installation of the organization must be able to see the
# repository.
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

# The organization of this project is bound to a GitLab Self-Managed group,
# and the token of that binding must be able to read the repository. The
# repository is the full path, which can hold subgroups.
resource "sonarqube_cloud_project_binding" "gitlab" {
  organization = "my-gitlab-organization"
  project_key  = "my-gitlab-organization_my-repo"
  repository   = "my-group/my-subgroup/my-repo"
}

# A destroy does not remove the binding. The API cannot do that: the binding
# goes away when the project is deleted.
