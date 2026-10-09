resource "sonarqube_group" "example" {
  organization = "my-organization"
  name         = "Developers"
  description  = "Developers with project access"
}
