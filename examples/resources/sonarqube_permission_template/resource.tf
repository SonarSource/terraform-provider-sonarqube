resource "sonarqube_group" "developers" {
  organization = "my-organization"
  name         = "Developers"
}

resource "sonarqube_permission_template" "backend" {
  organization        = "my-organization"
  name                = "Backend projects"
  description         = "Grants of the backend projects."
  project_key_pattern = "my-organization_backend-.*"

  project_creator_permissions = ["admin"]

  group {
    name        = sonarqube_group.developers.name
    permissions = ["user", "codeviewer", "issueadmin"]
  }

  user {
    login       = "alice@github"
    permissions = ["admin"]
  }
}
