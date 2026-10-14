resource "sonarqube_group" "developers" {
  organization = "my-organization"
  name         = "Developers"
}

resource "sonarqube_group_member" "alice" {
  organization = "my-organization"
  group_name   = sonarqube_group.developers.name
  login        = "alice@github"
}
