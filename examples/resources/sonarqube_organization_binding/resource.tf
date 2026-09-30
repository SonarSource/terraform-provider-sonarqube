resource "sonarqube_organization" "example" {
  key  = "my-organization"
  name = "My organization"
}

# Which GitHub application does this instance own? A binding accepts an
# installation of that application only.
data "sonarqube_dop_applications" "github" {
  dev_ops_platform = "github"
}

output "install_the_application_from" {
  value = [
    for application in data.sonarqube_dop_applications.github.applications :
    "https://github.com/apps/${application.application_key}"
  ]
}

# Install the application on the GitHub organization, then read the
# installation identifier from the address that GitHub shows afterwards:
# https://github.com/organizations/<github-org>/settings/installations/<installation_id>
resource "sonarqube_organization_binding" "example" {
  organization_key = sonarqube_organization.example.key
  installation_id  = "12345678"
}

# A destroy removes the organization and the binding together. The API cannot
# remove a binding on its own: the server drops it when it deletes the
# organization.
